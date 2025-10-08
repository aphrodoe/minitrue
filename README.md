# Minitrue - A Decentralized Time-Series Database

A decentralized, high-performance time-series database for IoT, built in Go.
## Core Features

- **Decentralized**: No single point of failure, leaderless architecture.
- **Scalable**: Horizontally scalable by simply adding more nodes.
- **High-Performance**: Built with Gorilla Compression and a custom storage format.
- **Resilient**: Survives node failures thanks to a Gossip protocol and data replication.

## Architecture Overview


## Getting Started

### Prerequisites

- Go 1.18+

### Installation & Running

```bash
# Clone the repository
git clone [https://github.com/aphrodoe/minitrue.git](https://github.com/aphrodoe/minitrue.git)
cd minitrue

# Build the server
go build -o server ./cmd/server

# Run the server
./server
```