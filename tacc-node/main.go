package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"tacc-node/consensus"
	"tacc-node/crypto"
	"tacc-node/network"
	"tacc-node/types"
)

var (
	engine   *consensus.ConsensusEngine
	nodePriv any
	nodeAddr string
)

func main() {
	port := flag.Int("port", 9000, "P2P port")
	rpc := flag.Int("rpc", 8080, "RPC port")
	id := flag.String("id", "node-1", "Node ID")
	peer := flag.String("peer", "", "Bootstrap peer multiaddr")
	mode := flag.String("mode", "tacc", "tacc or static")
	rho := flag.Float64("rho", 0.20, "Adversary fraction")
	epsilon := flag.Float64("epsilon", 1e-9, "Security epsilon")
	csvPath := flag.String("csv", filepath.Join("logs", "node.csv"), "CSV log path")
	flag.Parse()
	_ = os.MkdirAll("logs", 0755)

	priv, addr := crypto.GenerateKeyPair()
	nodePriv = priv
	nodeAddr = addr
	fmt.Println("==================================================")
	fmt.Printf("[INIT] TACC Node Starting | ID: [%s]\n", *id)
	fmt.Printf("[KEY]  Node Address:   %s\n", addr)
	fmt.Printf("[CONF] Mode: %s | Rho: %.2f | Epsilon: %g\n", *mode, *rho, *epsilon)
	fmt.Println("==================================================")

	p2p := network.NewP2PNode(*port)
	if *peer != "" {
		p2p.Connect(*peer)
	}
	engine = consensus.NewEngine(*id, p2p, *mode, *rho, *epsilon, *csvPath)
	engine.Balances[addr] = 10_000_000

	mux := http.NewServeMux()
	mux.HandleFunc("/submit", handleSubmit)
	mux.HandleFunc("/status", handleStatus)
	mux.HandleFunc("/metrics", handleMetrics)
	mux.HandleFunc("/events", handleEvents)
	mux.HandleFunc("/mode", handleMode)
	mux.HandleFunc("/fault", handleFault)
	mux.HandleFunc("/benchmark", handleBenchmark)
	mux.Handle("/", http.FileServer(http.Dir("dashboard")))

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", *rpc),
		Handler: withCORS(mux),
	}
	go func() {
		log.Printf("[RPC] Server listening on http://localhost:%d", *rpc)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	for round := 1; ; round++ {
		engine.RunRound(round)
		time.Sleep(800 * time.Millisecond)
	}
}

func handleSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var tx types.Transaction
	if err := json.NewDecoder(r.Body).Decode(&tx); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if tx.ID == "" {
		tx.ID = fmt.Sprintf("tx-%d", time.Now().UnixNano())
	}
	if tx.Timestamp == 0 {
		tx.Timestamp = time.Now().UnixMilli()
	}
	if len(tx.Signature) == 128 {
		msg := crypto.TxSigningMessage(tx.Sender, tx.Receiver, tx.Amount, tx.Data, tx.GasPrice)
		if !crypto.VerifySignature(tx.Sender, msg, tx.Signature) {
			http.Error(w, "invalid signature", http.StatusBadRequest)
			return
		}
	}
	engine.AddTx(tx)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "accepted",
		"txid":   tx.ID,
	})
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(engine.Status())
}

func handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(engine.MetricsSnapshot(200))
}

func handleMode(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var req struct {
			Mode string `json:"mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
			if req.Mode == "tacc" || req.Mode == "static" {
				engine.Ctrl.SetMode(req.Mode)
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"current_mode": engine.Ctrl.CurrentParams().Mode,
	})
}

func handleFault(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var fault network.NetworkFaultScenario
		if err := json.NewDecoder(r.Body).Decode(&fault); err == nil {
			engine.InjectFault(fault)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(engine.Netem.GetFaultConfig())
}

func handleBenchmark(w http.ResponseWriter, r *http.Request) {
	rounds := 30
	var fault network.NetworkFaultScenario
	fault.BurstMultiplier = 8.0
	fault.PacketLossRate = 0.10
	fault.DelayBurstActive = true

	report := consensus.RunComparativeBenchmark(rounds, fault)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(report)
}

func handleEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", http.StatusInternalServerError)
		return
	}
	ticker := time.NewTicker(1000 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			status := engine.Status()
			b, _ := json.Marshal(status)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", string(b))
			flusher.Flush()
		}
	}
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
