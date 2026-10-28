# TACC Experimental Benchmark Dataset and Replication Artifacts

This directory contains the empirical datasets, latency traces, and experimental configurations associated with the research paper:
**"TACC: Trilemma-Aware Adaptive Committee Consensus with Mathematical Security Constraints under Heavy-Tailed Network Latency"** (submitted to Elsevier *Journal of Parallel and Distributed Computing*).

---

## Dataset Description

### 1. `wan_latency_matrix.csv`
Contains the base inter-region Round Trip Time (RTT) matrix (in milliseconds) measured across a global topology distributed across five production Amazon Web Services (AWS) geographical regions:
- **US-West:** `us-west-2` (Oregon, United States)
- **US-East:** `us-east-1` (N. Virginia, United States)
- **Europe:** `eu-central-1` (Frankfurt, Germany)
- **Asia:** `ap-northeast-1` (Tokyo, Japan)
- **Africa:** `af-south-1` (Cape Town, South Africa)

**Empirical Source & Verification:**
- Cross-verified against AWS Global Infrastructure Performance telemetry and CloudPing (`cloudping.co`) median ping records.
- Values represent real physical subsea and terrestrial fiber routing bounds (Trans-Atlantic $\approx 85\text{--}90\,\text{ms}$, Trans-Pacific $\approx 115\text{--}125\,\text{ms}$, Cape Town long-haul $\approx 220\text{--}280\,\text{ms}$).
- Both `tacc-core-rust` and `tacc-node` dynamically load and parse this CSV file at runtime (`load_rtt_matrix_from_csv` / `LoadWANMatrixFromCSV`) with zero in-code hardcoded matrix literals.

### 2. `security_bounds_kmin.csv`
Contains the mathematically solved minimum committee sizes ($k_{\min}$) for varying Byzantine adversary fractions ($\rho \in [0.10, 0.33]$) under target security tolerance $\epsilon \le 10^{-9}$:
- Exact cumulative Binomial distribution model: $B(k, \rho)$
- Finite-population Hypergeometric distribution model: $H(N, M, k)$ for $N = 500$
- Serfling-Hoeffding analytical lower bounds

### 3. `round_execution_telemetry.csv`
Contains fine-grained round-by-round time-series data over 50 consecutive consensus rounds comparing the Static HotStuff-2 baseline against TACC Adaptive Consensus under synthetic network fault injection via Linux kernel `tc-netem`:
- Latency bursts: $8\times$ RTT delay spikes (up to 3,000 ms) injected at rounds 12-16, 28-32, and 42-45.
- Packet drops: 10% random packet drops.
- Columns record consensus latency, committed TPS, timeout occurrence, view-change transitions, dynamic timeout ($\Delta$), dynamic committee size ($k$), and block capacity ($B$).

### 4. `benchmark_summary.json`
Machine-readable summary of statistical metrics and performance comparisons:
- Mean latency, P90 latency, P99 tail latency
- Committed throughput (tx/s)
- View-change frequency (reconfigurations/minute)
- Consensus round completion success rate (%)

### 5. `smart_contract_gas_benchmarks.csv`
Contains empirical gas execution costs measured on the Ethereum Virtual Machine (EVM) for on-chain consensus settlement and verification smart contracts (`contracts/TACCVerifier.sol`, `contracts/TACCRewards.sol`):
- Aggregated Quorum Certificate (QC) signature verification ($k=25$ and $k=100$)
- RFC 9381 VRF non-interactive zero-knowledge proof verification
- Equivocation fraud proof challenges within the 64-block deferred slashing window
- Reward claim execution from validator escrow pools

---

## Replication and Toolchain

All off-chain and on-chain source code to reproduce these experiments is located in the parent directories:
- `tacc-core-rust/`: Zero-GC Tokio asynchronous consensus core and `tc-netem` latency emulator
- `tacc-node/`: Distributed Go consensus daemon, RFC 9381 VRF solver, and benchmark runner
- `contracts/`: Solidity smart contracts for on-chain verification and slashing
