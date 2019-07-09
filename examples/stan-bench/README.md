## NATS Streaming Benchmarks

### Running the server

#### This is in-memory storage
```bash
> nats-streaming-server
```

#### This is using file based storage
```bash
> nats-streaming-server -c filestore.conf
```

### Running the Benchmark

See usage help for more info `./stan-bench -h`.
Example results with file based storage configuration.

```bash
# Publish Only
> ./stan-bench -np 100 -n 1000000 -ms 1024 foo

Starting benchmark [msgs=1000000, msgsize=1024, pubs=100, subs=0]
Pub stats: 386,193 msgs/sec ~ 377.14 MB/sec

# Subscribe Only
> ./stan-bench -np 0 -ns 1 -n 1000000 -ms 1024 foo

Starting benchmark [msgs=1000000, msgsize=1024, pubs=0, subs=1]
Sub stats: 188,925 msgs/sec ~ 184.50 MB/sec

# Multiple Queue Subscribers
> ./stan-bench -np 0 -ns 100 -qgroup T -n 1000000 -ms 1024 foo
Starting benchmark [msgs=1000000, msgsize=1024, pubs=0, subs=100]
Sub stats: 111,711 msgs/sec ~ 109.09 MB/sec

# Streaming
> ./stan-bench -np 1 -ns 1 -n 1000000 -ms 1024 bar

Starting benchmark [msgs=1000000, msgsize=1024, pubs=1, subs=1]
NATS Streaming Pub/Sub stats: 413,665 msgs/sec ~ 403.97 MB/sec
 Pub stats: 206,832 msgs/sec ~ 201.98 MB/sec
 Sub stats: 206,869 msgs/sec ~ 202.02 MB/sec
```
