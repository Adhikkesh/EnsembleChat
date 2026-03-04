# ZooKeeper-Inspired Distributed Chat Server Coordination System

A Go-based distributed coordination system implementing three fundamental distributed systems algorithms: **Leader Election (Simplified Raft)**, **Lease-Based Distributed Locking**, and **Two-Phase Commit (2PC)** — applied to a globally distributed chat routing system.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                        SYSTEM ARCHITECTURE                             │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                         │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐       LAYER 1: CLIENTS       │
│  │ Client A │  │ Client B │  │ Client C │       (End Users)             │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘                              │
│       │              │              │                                    │
│  ─────┼──────────────┼──────────────┼────────────────────────────────── │
│       │              │              │                                    │
│  ┌────▼─────┐  ┌─────▼────┐  ┌─────▼────┐  LAYER 2: CHAT SERVERS      │
│  │ Server 1 │  │ Server 2 │  │ Server 3 │  (Stateless Workers)         │
│  │ :9001    │  │ :9002    │  │ :9003    │                              │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘                              │
│       │              │              │                                    │
│  ─────┼──────────────┼──────────────┼────────────────────────────────── │
│       │              │              │                                    │
│  ┌────▼──────────────▼──────────────▼────┐  LAYER 3: COORDINATION      │
│  │        COORDINATION ENSEMBLE          │  ENSEMBLE (ZK Nodes)        │
│  │                                        │                              │
│  │  ┌──────────┐ ┌──────────┐ ┌────────┐ │                              │
│  │  │  Node 0  │ │  Node 1  │ │ Node 2 │ │  3 Coordinator Nodes       │
│  │  │ (LEADER) │ │(FOLLOWER)│ │(FOLLOW)│ │                              │
│  │  │          │ │          │ │        │ │  ┌──────────────────────┐   │
│  │  │ • Election│ │ • Election│ │• Elect │ │  │ Replicated State:    │   │
│  │  │ • Locks   │ │ • 2PC Part│ │• 2PC  │ │  │  • Routing Table     │   │
│  │  │ • 2PC Coord│ │          │ │       │ │  │  • Server Registry   │   │
│  │  └──────────┘ └──────────┘ └────────┘ │  │  • Room Mappings     │   │
│  │                                        │  └──────────────────────┘   │
│  └────────────────────────────────────────┘                              │
└─────────────────────────────────────────────────────────────────────────┘
```

## Algorithms Implemented

### 1. Leader Election — Simplified Raft Protocol
**File:** `internal/election/election.go`

| Concept | Implementation |
|---------|---------------|
| State Machine | Follower → Candidate → Leader |
| Failure Detection | Randomized heartbeat timeouts (300-500ms) |
| Voting | Term-based, first-come-first-served |
| Recovery | Automatic re-election on leader crash |

**Flow:**
```
Follower ──(timeout)──► Candidate ──(majority vote)──► Leader
    ▲                       │                              │
    │                       ├──(higher term seen)──────────┘
    │                       │                      (sends heartbeats)
    └──(heartbeat received)─┘
```

### 2. Mutual Exclusion — Lease-Based Distributed Locking
**File:** `internal/lock/lock.go`

| Concept | Implementation |
|---------|---------------|
| Lock Type | Time-bound leases (configurable TTL) |
| Deadlock Prevention | Automatic lease expiry on holder crash |
| Ownership | Only the holder can release a lock |
| Contention | Immediate rejection if resource is locked |

**Flow:**
```
Server A ──► AcquireLock("General_Chat") ──► GRANTED (lease 10s)
Server B ──► AcquireLock("General_Chat") ──► DENIED (held by Server A)
             ... Server A releases or lease expires ...
Server B ──► AcquireLock("General_Chat") ──► GRANTED
```

### 3. Consensus — Two-Phase Commit (2PC)
**File:** `internal/consensus/consensus.go`

| Concept | Implementation |
|---------|---------------|
| Phase 1 | Leader sends PREPARE → Participants vote COMMIT/ABORT |
| Phase 2a | All COMMIT → Leader sends COMMIT → All apply change |
| Phase 2b | Any ABORT → Leader sends ABORT → No changes applied |
| Timeout | Unresponsive participants trigger ABORT (3s timeout) |

**Flow:**
```
Leader (Coordinator)          Follower 1 (Participant)    Follower 2 (Participant)
       │                              │                           │
       ├──── PREPARE ────────────────►│                           │
       ├──── PREPARE ──────────────────────────────────────────►│
       │                              │                           │
       │◄──── VOTE_COMMIT ───────────┤                           │
       │◄──── VOTE_COMMIT ────────────────────────────────────── │
       │                              │                           │
       ├──── COMMIT ─────────────────►│                           │
       ├──── COMMIT ───────────────────────────────────────────►│
       │                              │                           │
   [Applied]                      [Applied]                   [Applied]
```

## Project Structure

```
├── go.mod                              # Go module definition
├── README.md                           # This file
├── internal/
│   ├── types/types.go                  # Shared types, enums, message definitions
│   ├── transport/transport.go          # JSON-over-TCP networking layer
│   ├── election/election.go            # Algorithm 1: Simplified Raft Election
│   ├── lock/lock.go                    # Algorithm 2: Lease-Based Locking
│   ├── consensus/consensus.go          # Algorithm 3: Two-Phase Commit
│   ├── coordinator/coordinator.go      # Coordination ensemble node
│   ├── chatserver/chatserver.go        # Chat server worker node
│   └── client/client.go               # Chat client
├── cmd/
│   ├── coordinator/main.go             # Coordinator node launcher
│   ├── chatserver/main.go              # Chat server launcher
│   └── client/main.go                  # Client launcher
└── demo/
    ├── demo_leader_failure/main.go     # Election & failure recovery demo
    ├── demo_mutual_exclusion/main.go   # Lock contention demo
    └── demo_consensus/main.go          # 2PC state replication demo
```

## Quick Start

### Prerequisites
- Go 1.21+ installed ([download](https://go.dev/dl/))

### Run the Demos

Each demo is a self-contained script that simulates a specific distributed algorithm:

```bash
# Demo 1: Leader Election & Failure Recovery
# Shows: 3 nodes elect a leader → leader is killed → new leader elected
go run ./demo/demo_leader_failure/

# Demo 2: Mutual Exclusion (Lease-Based Locking)
# Shows: 2 servers race for same lock → one wins, one denied → release → retry
go run ./demo/demo_mutual_exclusion/

# Demo 3: Consensus (Two-Phase Commit)
# Shows: Leader proposes change → all nodes vote → committed → state replicated
go run ./demo/demo_consensus/
```

### Run Individual Components

```bash
# Start a coordinator node
go run ./cmd/coordinator/ -id=0 -addr=:7000

# Start a chat server
go run ./cmd/chatserver/ -id=server1 -addr=:9001

# Start a client
go run ./cmd/client/ -id=alice
```

## Real-World System Mapping

This project maps directly to production distributed systems used at global scale:

| Our System | ZooKeeper / etcd | WhatsApp / Discord |
|-----------|-----------------|-------------------|
| Coordination Ensemble | ZooKeeper ensemble (3-5 nodes) | Not directly exposed, but similar internal coordination |
| Leader Election (Raft) | ZAB protocol (ZooKeeper), Raft (etcd) | Used internally for partition leadership |
| Lease-Based Locking | Ephemeral nodes + session timeouts (ZK), Lease API (etcd) | Room creation locks, user session management |
| Two-Phase Commit | Atomic broadcast (ZAB), Raft log replication | Message delivery guarantees, group membership changes |
| Chat Servers | Application servers (stateless workers) | Chat server fleet (~1000s of servers) |
| Routing Table | `/services/routing` znode tree | Consistent hashing ring for user→server mapping |
| Heartbeat Detection | Session keepalive, leader pings | Server health monitoring, connection liveness |

### Detailed Mappings

**ZooKeeper:**
- Our `ElectionNode` → ZooKeeper's ZAB leader election. ZK uses a similar term-based protocol to elect a single leader among ensemble nodes.
- Our `LockManager` → ZooKeeper's recipe for distributed locks using ephemeral sequential znodes. Our lease TTL maps to ZK's session timeout.
- Our `Coordinator.Propose()` → ZooKeeper's atomic broadcast. When a client writes to ZK, the leader broadcasts to followers and waits for majority acknowledgment.

**etcd / Kubernetes:**
- Our Raft election → etcd uses the full Raft protocol. Our simplified version omits log replication but captures the core election mechanics.
- Our lease-based locks → etcd's `Lease` API, used by Kubernetes for leader election among controller-manager pods. The TTL prevents split-brain when a pod crashes.
- Our 2PC → etcd's Raft log replication ensures all nodes agree on the same sequence of operations.

**WhatsApp at Scale:**
- Our chat servers → WhatsApp runs ~1000+ Erlang servers, each handling a subset of conversations.
- Our routing table → WhatsApp uses consistent hashing to map phone numbers to servers. Our coordinator ensemble serves a similar purpose.
- Our lock mechanism → When creating a group chat, WhatsApp must ensure no two servers create the same group simultaneously — analogous to our lease-based locking.

## Design Decisions

1. **In-Process Communication for Demos:** Demos use direct Go channel communication instead of TCP to keep them self-contained and easy to run. The production `cmd/` launchers use the TCP transport layer.

2. **Simplified Raft:** We omit log replication (handled separately by 2PC) and log-based vote comparison for clarity. The election mechanics (terms, timeouts, voting) are preserved.

3. **Lease-Based Locking vs. Queue-Based:** We chose leases over a queue-based approach because leases inherently handle holder crashes via TTL expiry, making the system more resilient.

4. **2PC vs. Raft Log Replication:** While production systems like etcd replicate state via Raft logs, we use 2PC to demonstrate a distinct consensus algorithm and emphasize the prepare/commit/abort phases.

## Terminal Output

All demos produce color-coded, timestamped terminal output with emoji prefixes for easy visual tracking:

- 🟢 `[Node X | ELECTION]` — Election events
- 🔵 `[Node X | LOCK]` — Lock operations
- 🟣 `[Node X | 2PC-COORD]` — 2PC coordinator events
- 🟡 `[Node X | 2PC-PART]` — 2PC participant events
- 👑 — Leader election wins
- ❌ — Denials, aborts, failures
- ✅ — Grants, commits, successes

---

*Built for DS Case Study — Distributed Systems, Semester 6*
