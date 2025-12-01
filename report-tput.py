#!/usr/bin/env python3

import os
import glob
import re
import sys
import statistics

def parse_latency(val_str):
    val_str = val_str.strip()
    mult = 1.0
    if "µs" in val_str or "us" in val_str:
        val_str = val_str.replace("µs", "").replace("us", "")
        mult = 1.0
    elif "ms" in val_str:
        val_str = val_str.replace("ms", "")
        mult = 1000.0
    elif "s" in val_str:
        val_str = val_str.replace("s", "")
        mult = 1000000.0
    
    try:
        return float(val_str) * mult
    except ValueError:
        return None

def main():
    if len(sys.argv) > 1:
        LOG_DIR = sys.argv[1]
    else:
        # Default to latest in logs-rdma if not specified (legacy behavior)
        # But check if it exists
        if os.path.exists("./logs-rdma/latest"):
            LOG_DIR = "./logs-rdma/latest"
        elif os.path.exists("./baseline/logs/latest"):
            LOG_DIR = "./baseline/logs/latest"
        else:
            print("No log directory found. Usage: python3 report-tput.py <log_dir>")
            return

    print(f"Parsing logs in: {LOG_DIR}")

    # --- Throughput (from Server Logs) ---
    # Baseline: kvsserver-*.log containing "ops/s 123.45"
    # RDMA: server-*.log (RDMA server doesn't print ops/s periodically in current code? 
    #       Wait, RDMA server logs might not have throughput. 
    #       RDMA benchmark usually relies on client reporting.
    #       Let's check client logs for RDMA throughput.)
    
    server_tput = 0.0
    
    # Try Baseline Server Logs
    server_logs = glob.glob(os.path.join(LOG_DIR, "kvsserver-*.log"))
    if server_logs:
        print("\nServer Throughput (Baseline):")
        for log_path in sorted(server_logs):
            node = os.path.basename(log_path).replace("kvsserver-", "").replace(".log", "")
            tputs = []
            with open(log_path, 'r') as f:
                for line in f:
                    if "ops/s" in line:
                        try:
                            # Format: "ops/s 123.45"
                            parts = line.strip().split()
                            if parts[0] == "ops/s":
                                tputs.append(float(parts[1]))
                        except:
                            pass
            
            if tputs:
                # Use median to avoid startup/shutdown noise
                median_tput = statistics.median(tputs)
                print(f"{node}: {median_tput:.2f} ops/s")
                server_tput += median_tput
            else:
                print(f"{node}: No data")
    
    # --- Throughput (from Client Logs - RDMA & Baseline fallback) ---
    # RDMA Client: "Throughput: 123.45 ops/sec"
    # Baseline Client: "throughput 123.45 ops/s"
    
    client_tput = 0.0
    client_logs = glob.glob(os.path.join(LOG_DIR, "client-*.log")) + glob.glob(os.path.join(LOG_DIR, "kvsclient-*.log"))
    
    latencies = []
    
    if client_logs:
        print("\nClient Metrics:")
        print(f"{'Node':<10} {'Throughput':<15} {'Latency (us)':<15}")
        
        for log_path in sorted(client_logs):
            node = os.path.basename(log_path).replace("client-", "").replace("kvsclient-", "").replace(".log", "")
            tput = 0.0
            lat = 0.0
            
            with open(log_path, 'r') as f:
                content = f.read()
                
                # RDMA Format
                m_rdma_tput = re.search(r"Throughput:\s+([\d\.]+)\s+ops/sec", content)
                m_rdma_lat = re.search(r"Avg Latency:\s+(.+)", content)
                
                # Baseline Format
                m_base_tput = re.search(r"throughput\s+([\d\.]+)\s+ops/s", content)
                m_base_lat = re.search(r"avg batch latency\s+(.+)", content)
                
                if m_rdma_tput:
                    tput = float(m_rdma_tput.group(1))
                elif m_base_tput:
                    tput = float(m_base_tput.group(1))
                    
                if m_rdma_lat:
                    lat = parse_latency(m_rdma_lat.group(1))
                elif m_base_lat:
                    lat = parse_latency(m_base_lat.group(1))
            
            if tput > 0:
                client_tput += tput
            if lat is not None and lat > 0:
                latencies.append(lat)
            
            print(f"{node:<10} {tput:<15.2f} {lat if lat else 'N/A':<15}")

    print("-" * 40)
    
    # Decide which throughput to report
    # If we have server throughput (Baseline), use it as it's often more accurate aggregate.
    # If not, use client sum.
    final_tput = server_tput if server_tput > 0 else client_tput
    
    print(f"Total Throughput: {final_tput:.2f} ops/s")
    
    if latencies:
        avg_lat = sum(latencies) / len(latencies)
        print(f"Avg Latency:      {avg_lat:.2f} us")
    else:
        print("Avg Latency:      N/A")

if __name__ == "__main__":
    main()
