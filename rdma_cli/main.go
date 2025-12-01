package main

import (
	"flag"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"kv-rdma/kv_rdma/kv"
	"kv-rdma/kv_rdma/kvs"
)

func main() {
	mode := flag.String("mode", "server", "Mode: server, client, or bench")
	addr := flag.String("addr", ":8080", "Address to listen on (server)")
	hosts := flag.String("hosts", "", "Comma-separated list of server addresses (client/bench)")
	dev := flag.String("dev", "", "RDMA device name (optional)")
	ibPort := flag.Int("ib-port", 1, "IB port number")
	gidIndex := flag.Int("gid-index", 0, "GID index")
	duration := flag.Duration("duration", 10*time.Second, "Benchmark duration")
	workers := flag.Int("workers", 1, "Number of concurrent workers")
	batchSize := flag.Int("batch-size", 16, "Batch size for operations")
	flag.Parse()

	if *mode == "server" {
		runServer(*dev, *ibPort, *gidIndex, *addr)
	} else if *mode == "client" {
		// For simple client test, we can use hosts or addr
		targetHosts := []string{}
		if *hosts != "" {
			targetHosts = strings.Split(*hosts, ",")
		} else {
			targetHosts = []string{*addr}
		}
		runClient(*dev, targetHosts, *ibPort, *gidIndex)
	} else if *mode == "bench" {
		targetHosts := []string{}
		if *hosts != "" {
			targetHosts = strings.Split(*hosts, ",")
		} else {
			targetHosts = []string{*addr}
		}
		runBenchmark(*dev, targetHosts, *duration, *ibPort, *gidIndex, *workers, *batchSize)
	} else {
		log.Fatalf("Unknown mode: %s", *mode)
	}
}

func runServer(dev string, port, gidIndex int, addr string) {
	server, err := kv.NewServer(dev, port, gidIndex)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	log.Printf("Starting server on %s...", addr)
	if err := server.Start(addr); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func runClient(dev string, hosts []string, port, gidIndex int) {
	client, err := kv.NewClient(dev, port, gidIndex)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	log.Printf("Connecting to %v...", hosts)
	if err := client.Connect(hosts); err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	log.Println("Connected!")

	// Simple test
	log.Println("Performing Put...")
	data := []byte("Hello World!")
	if err := client.Put(0, data); err != nil {
		log.Fatalf("Put failed: %v", err)
	}

	log.Println("Performing Get...")
	val, err := client.Get(0, len(data))
	if err != nil {
		log.Fatalf("Get failed: %v", err)
	}
	log.Printf("Got: %s", string(val))

	if string(val) != string(data) {
		log.Fatalf("Mismatch! Expected %s, got %s", string(data), string(val))
	}
	log.Println("Success!")
}

func runBenchmark(dev string, hosts []string, duration time.Duration, port, gidIndex, workers, batchSize int) {
	var wg sync.WaitGroup
	opsCh := make(chan int, workers)
	latCh := make(chan time.Duration, 1000000) // Buffer for latencies

	log.Printf("Starting benchmark with %d workers, batch size %d on %v...", workers, batchSize, hosts)

	start := time.Now()

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Each worker needs its own client and QP/CQ/MR
			client, err := kv.NewClient(dev, port, gidIndex)
			if err != nil {
				log.Printf("Worker %d failed to create client: %v", id, err)
				return
			}
			defer client.Close()

			if err := client.Connect(hosts); err != nil {
				log.Printf("Worker %d failed to connect: %v", id, err)
				return
			}

			workload := kvs.NewWorkload("YCSB-B", 0.99)
			payload := make([]byte, 100)

			localOps := 0
			var localLatencies []time.Duration

			for time.Since(start) < duration {
				// Collect batch
				var readKeys []int
				var writeKeys []int
				var writeData [][]byte

				batchStart := time.Now()

				for j := 0; j < batchSize; j++ {
					op := workload.Next()
					if op.IsRead {
						readKeys = append(readKeys, int(op.Key*100))
					} else {
						writeKeys = append(writeKeys, int(op.Key*100))
						writeData = append(writeData, payload)
					}
				}

				// Execute batches
				if len(readKeys) > 0 {
					_, err := client.GetBatch(readKeys, 100)
					if err != nil {
						log.Printf("Worker %d GetBatch failed: %v", id, err)
						continue
					}
				}

				if len(writeKeys) > 0 {
					err := client.PutBatch(writeKeys, writeData)
					if err != nil {
						log.Printf("Worker %d PutBatch failed: %v", id, err)
						continue
					}
				}

				batchLat := time.Since(batchStart)
				avgOpLat := batchLat / time.Duration(batchSize)
				for k := 0; k < batchSize; k++ {
					localLatencies = append(localLatencies, avgOpLat)
				}
				localOps += batchSize
			}
			opsCh <- localOps

			// Calculate local average latency
			var totalLat time.Duration
			for _, l := range localLatencies {
				totalLat += l
			}
			if len(localLatencies) > 0 {
				latCh <- totalLat / time.Duration(len(localLatencies))
			}
			log.Printf("Worker %d finished. Ops: %d", id, localOps)
		}(i)
	}

	log.Println("Waiting for workers...")
	wg.Wait()
	log.Println("Workers finished.")
	elapsed := time.Since(start)
	close(opsCh)
	close(latCh)

	totalOps := 0
	for ops := range opsCh {
		totalOps += ops
	}

	var totalAvgLat time.Duration
	count := 0
	for lat := range latCh {
		totalAvgLat += lat
		count++
	}
	avgLat := time.Duration(0)
	if count > 0 {
		avgLat = totalAvgLat / time.Duration(count)
	}

	throughput := float64(totalOps) / elapsed.Seconds()
	log.Printf("DEBUG: Elapsed: %v, TotalOps: %d, Throughput: %f", elapsed, totalOps, throughput)

	fmt.Printf("Benchmark Results:\n")
	fmt.Printf("Duration: %v\n", elapsed)
	fmt.Printf("Operations: %d\n", totalOps)
	fmt.Printf("Throughput: %.2f ops/sec\n", throughput)
	fmt.Printf("Avg Latency: %v\n", avgLat)
}
