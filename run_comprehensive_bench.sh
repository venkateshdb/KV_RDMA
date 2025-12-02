#!/bin/bash

# Configuration
DURATION=30
DEFAULT_BATCH_SIZE=64
WORKERS=16
OUTPUT_FILE="benchmark_results.csv"
BATCH_SIZES=(1 4 16 64)


NODES=($( /usr/local/etc/emulab/tmcc hostnames | awk -F"ALIASES='" '{print $2}' | awk '{print $NF}' | sed "s/'//g" | sort ))
TOTAL_NODES=${#NODES[@]}

echo "$TOTAL_NODES nodes: ${NODES[@]}"

# Initialize Output File
echo "Type,Servers,Clients,BatchSize,Workers,Throughput(ops/s),Latency(us),ServerCPU(%),NetBW(MB/s)" > $OUTPUT_FILE

cleanup() {
    echo "Cleaning up..."
    for node in "${NODES[@]}"; do
        ssh -o StrictHostKeyChecking=no $node "pkill -f kv-rdma; pkill -f server; pkill -f client; pkill -f kvsserver; pkill -f kvsclient" > /dev/null 2>&1
    done
}

# Helper to get CPU stats
get_cpu_stat() {
    ssh -o StrictHostKeyChecking=no $1 "grep '^cpu ' /proc/stat"
}

calc_cpu_usage() {
    local start_stat="$1"
    local end_stat="$2"
    
    if [ -z "$start_stat" ] || [ -z "$end_stat" ]; then
        echo "0"
        return
    fi
    
    read -r _ user1 nice1 system1 idle1 iowait1 irq1 softirq1 steal1 _ <<< "$start_stat"
    local total1=$((user1 + nice1 + system1 + idle1 + iowait1 + irq1 + softirq1 + steal1))
    local work1=$((user1 + nice1 + system1 + irq1 + softirq1 + steal1))
    
    read -r _ user2 nice2 system2 idle2 iowait2 irq2 softirq2 steal2 _ <<< "$end_stat"
    local total2=$((user2 + nice2 + system2 + idle2 + iowait2 + irq2 + softirq2 + steal2))
    local work2=$((user2 + nice2 + system2 + irq2 + softirq2 + steal2))
    
    local total_delta=$((total2 - total1))
    local work_delta=$((work2 - work1))
    
    if [ $total_delta -eq 0 ]; then
        echo "0"
    else
        echo | awk "{printf \"%.2f\", ($work_delta * 100) / $total_delta}"
    fi
}

# Helper to get Network stats (Bytes RX + TX for eno1d1)
get_net_stat() {
    ssh -o StrictHostKeyChecking=no $1 "grep 'eno1d1' /proc/net/dev" | awk '{print $2 + $10}'
}

# Helper to calculate Bandwidth (MB/s)
calc_net_bw() {
    local start_bytes="$1"
    local end_bytes="$2"
    local duration="$3"
    
    if [ -z "$start_bytes" ] || [ -z "$end_bytes" ]; then
        echo "0"
        return
    fi
    
    local bytes_delta=$((end_bytes - start_bytes))
    # Convert to MB/s
    echo | awk "{printf \"%.2f\", ($bytes_delta / 1024 / 1024) / $duration}"
}

# Helper to parse Latency to Microseconds
parse_latency() {
    local val=$1
    local unit=$2
    
    # Remove non-numeric chars from val just in case
    val=$(echo $val | sed 's/[^0-9.]//g')
    
    if [[ "$unit" == *"ms"* ]]; then
        echo | awk "{printf \"%.2f\", $val * 1000}"
    elif [[ "$unit" == *"us"* ]] || [[ "$unit" == *"µs"* ]]; then
        echo $val
    elif [[ "$unit" == *"s"* ]]; then
         echo | awk "{printf \"%.2f\", $val * 1000000}"
    else
        # Assume us if no unit or unknown
        echo $val
    fi
}


echo "Building..."
cd baseline
go build -o kv-baseline-server ./kvs/server/main.go
go build -o kv-baseline-client ./kvs/client/main.go
cd ..
go build -o kv-rdma ./rdma_cli/main.go

echo "========================================"
echo "Starting TCP Benchmarks"
echo "========================================"

for current_batch_size in "${BATCH_SIZES[@]}"; do
    echo "--- Running TCP Benchmarks with Batch Size: $current_batch_size ---"
    for scale in 1 2 4; do
        echo "Running TCP Scale: ${scale}x${scale}"
        
        servers=("${NODES[@]:0:$scale}")
        clients=("${NODES[@]:$scale:$scale}")
        
        cleanup
        

        cpu_starts=()
        net_starts=()
        for node in "${servers[@]}"; do
            cpu_starts+=("$(get_cpu_stat $node)")
            net_starts+=("$(get_net_stat $node)")
        done
        
        # Start Servers
        for i in "${!servers[@]}"; do
            node=${servers[$i]}
            port=$((8080 + i))
            ssh -o StrictHostKeyChecking=no $node "/mnt/nfs/KV_RDMA/baseline/kv-baseline-server -port $port" > /dev/null 2>&1 &
        done
        
        sleep 2

        client_pids=()
        log_dir="logs/tcp_${scale}_batch_${current_batch_size}"
        mkdir -p $log_dir
        
        for i in "${!clients[@]}"; do
            node=${clients[$i]}
            hosts=""
            for j in "${!servers[@]}"; do
                if [ $j -gt 0 ]; then hosts+=","; fi
                hosts+="${servers[$j]}:$((8080 + j))"
            done
            
            ssh -o StrictHostKeyChecking=no $node "/mnt/nfs/KV_RDMA/baseline/kv-baseline-client -hosts $hosts -secs ${DURATION} -workers ${WORKERS} -batch_size ${current_batch_size}" > "$log_dir/client_${node}.log" 2>&1 &
            client_pids+=($!)
        done
        
       
        for pid in "${client_pids[@]}"; do
            wait $pid
        done
        

        cpu_ends=()
        net_ends=()
        for node in "${servers[@]}"; do
            cpu_ends+=("$(get_cpu_stat $node)")
            net_ends+=("$(get_net_stat $node)")
        done
        
        # Calculate Metrics
        total_cpu=0
        total_bw=0
        for i in "${!servers[@]}"; do
            usage=$(calc_cpu_usage "${cpu_starts[$i]}" "${cpu_ends[$i]}")
            total_cpu=$(echo $total_cpu $usage | awk '{print $1 + $2}')
            
            bw=$(calc_net_bw "${net_starts[$i]}" "${net_ends[$i]}" "$DURATION")
            total_bw=$(echo $total_bw $bw | awk '{print $1 + $2}')
        done
        avg_cpu=$(echo $total_cpu $scale | awk '{printf "%.2f", $1 / $2}')
        
     
        total_tput=0
        total_lat=0
        count=0
        for log in $log_dir/*.log; do
            tput=$(grep "throughput" $log | awk '{print $2}')
            lat_val=$(grep "avg batch latency" $log | awk '{print $4}' | sed 's/[^0-9.]//g')
            lat_unit=$(grep "avg batch latency" $log | awk '{print $4}' | sed 's/[0-9.]//g')
            
            if [ ! -z "$tput" ]; then
                total_tput=$(echo $total_tput $tput | awk '{print $1 + $2}')
                lat_us=$(parse_latency "$lat_val" "$lat_unit")
                total_lat=$(echo $total_lat $lat_us | awk '{print $1 + $2}')
                count=$((count + 1))
            fi
        done
        
        if [ $count -gt 0 ]; then
            avg_lat=$(echo $total_lat $count | awk '{printf "%.2f", $1 / $2}')
        else
            avg_lat=0
        fi
        
        echo "TCP Result: Tput=$total_tput, Lat=$avg_lat us, CPU=$avg_cpu %, BW=$total_bw MB/s"
        echo "TCP,$scale,$scale,$current_batch_size,$WORKERS,$total_tput,$avg_lat,$avg_cpu,$total_bw" >> $OUTPUT_FILE
        
        cleanup
        sleep 5
    done
done


echo "========================================"
echo "Starting RDMA Benchmarks"
echo "========================================"

for current_batch_size in "${BATCH_SIZES[@]}"; do
    echo "--- Running RDMA Benchmarks with Batch Size: $current_batch_size ---"
    for scale in 1 2 4; do
        echo "Running RDMA Scale: ${scale}x${scale}"
        
        servers=("${NODES[@]:0:$scale}")
        clients=("${NODES[@]:$scale:$scale}")
        
        cleanup
        
        cpu_starts=()
        net_starts=()
        for node in "${servers[@]}"; do
            cpu_starts+=("$(get_cpu_stat $node)")
            net_starts+=("$(get_net_stat $node)")
        done
        
        RDMA_PORT=8090
        log_dir="logs/rdma_${scale}_batch_${current_batch_size}"
        mkdir -p $log_dir
        
        for i in "${!servers[@]}"; do
            node=${servers[$i]}
            ssh -o StrictHostKeyChecking=no $node "/mnt/nfs/KV_RDMA/kv-rdma -mode server -addr :$RDMA_PORT -dev mlx4_0 -ib-port 2 -gid-index 2" > "$log_dir/server_${node}.log" 2>&1 &
        done
        
        sleep 2
        
        client_pids=()
        hosts=""
        for j in "${!servers[@]}"; do
            if [ $j -gt 0 ]; then hosts+=","; fi
            hosts+="${servers[$j]}:$RDMA_PORT"
        done
            
        for i in "${!clients[@]}"; do
            node=${clients[$i]}
            ssh -o StrictHostKeyChecking=no $node "/mnt/nfs/KV_RDMA/kv-rdma -mode bench -hosts $hosts -batch-size ${current_batch_size} -workers ${WORKERS} -duration ${DURATION}s -dev mlx4_0 -ib-port 2 -gid-index 2" > "$log_dir/client_${node}.log" 2>&1 &
            client_pids+=($!)
        done
        
        for pid in "${client_pids[@]}"; do
            wait $pid
        done
        cpu_ends=()
        net_ends=()
        for node in "${servers[@]}"; do
            cpu_ends+=("$(get_cpu_stat $node)")
            net_ends+=("$(get_net_stat $node)")
        done
        
        total_cpu=0
        total_bw=0
        for i in "${!servers[@]}"; do
            usage=$(calc_cpu_usage "${cpu_starts[$i]}" "${cpu_ends[$i]}")
            total_cpu=$(echo $total_cpu $usage | awk '{print $1 + $2}')
            
            bw=$(calc_net_bw "${net_starts[$i]}" "${net_ends[$i]}" "$DURATION")
            total_bw=$(echo $total_bw $bw | awk '{print $1 + $2}')
        done
        avg_cpu=$(echo $total_cpu $scale | awk '{printf "%.2f", $1 / $2}')
        
        
        total_tput=0
        total_lat=0
        count=0
        for log in $log_dir/*.log; do
        
            tput=$(grep "Throughput:" $log | awk '{print $2}')
            lat_val=$(grep "Avg Latency:" $log | awk '{print $3}' | sed 's/[^0-9.]//g')
            lat_unit=$(grep "Avg Latency:" $log | awk '{print $3}' | sed 's/[0-9.]//g')
            
            if [ ! -z "$tput" ]; then
                total_tput=$(echo $total_tput $tput | awk '{print $1 + $2}')
                lat_us=$(parse_latency "$lat_val" "$lat_unit")
                total_lat=$(echo $total_lat $lat_us | awk '{print $1 + $2}')
                count=$((count + 1))
            fi
        done
        
        if [ $count -gt 0 ]; then
            avg_lat=$(echo $total_lat $count | awk '{printf "%.2f", $1 / $2}')
        else
            avg_lat=0
        fi
        
        echo "RDMA Result: Tput=$total_tput, Lat=$avg_lat us, CPU=$avg_cpu %, BW=$total_bw MB/s"
        echo "RDMA,$scale,$scale,$current_batch_size,$WORKERS,$total_tput,$avg_lat,$avg_cpu,$total_bw" >> $OUTPUT_FILE
        
        cleanup
    done
done

echo "Benchmark Complete. Results in $OUTPUT_FILE"
cat $OUTPUT_FILE
