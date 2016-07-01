// Copyright 2015 Apcera Inc. All rights reserved.
// +build ignore

package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/go-nats-streaming"
	"github.com/nats-io/nats"
)

// Some sane defaults
const (
	DefaultNumMsgs            = 100000
	DefaultNumPubs            = 1
	DefaultNumSubs            = 0
	DefaultAsync              = false
	DefaultMessageSize        = -1
	DefaultIgnoreOld          = false
	DefaultMaxPubAcksInflight = 1000
	DefaultClientID           = "benchmark"
)

func usage() {
	log.Fatalf("Usage: nats-bench [-s server (%s)] [--tls] [-id CLIENT_ID] [-np NUM_PUBLISHERS] [-ns NUM_SUBSCRIBERS] [-n NUM_MSGS] [-ms MESSAGE_SIZE] [-io] [-a] <subject>\n", nats.DefaultURL)
}

var subStatChan chan *stats
var pubStatChan chan *stats

func main() {
	var urls = flag.String("s", nats.DefaultURL, "The nats server URLs (separated by comma)")
	var tls = flag.Bool("tls", false, "Use TLS Secure Connection")
	var numPubs = flag.Int("np", DefaultNumPubs, "Number of Concurrent Publishers")
	var numSubs = flag.Int("ns", DefaultNumSubs, "Number of Concurrent Subscribers")
	var numMsgs = flag.Int("n", DefaultNumMsgs, "Number of Messages to Publish")
	var async = flag.Bool("a", DefaultAsync, "Async Message Publishing")
	var messageSize = flag.Int("ms", DefaultMessageSize, "Message Size in bytes.")
	var ignoreOld = flag.Bool("io", DefaultIgnoreOld, "Subscribers Ignore Old Messages")
	var maxPubAcks = flag.Int("mpa", DefaultMaxPubAcksInflight, "Max number of published acks in flight")
	var clientID = flag.String("id", DefaultClientID, "Benchmark process base client ID.")

	log.SetFlags(0)
	flag.Usage = usage
	flag.Parse()

	args := flag.Args()
	if len(args) != 1 {
		usage()
	}

	// Setup the option block
	opts := nats.DefaultOptions
	opts.Servers = strings.Split(*urls, ",")
	for i, s := range opts.Servers {
		opts.Servers[i] = strings.Trim(s, " ")
	}
	opts.Secure = *tls

	var startwg sync.WaitGroup
	var donewg sync.WaitGroup

	subStatChan = make(chan *stats, *numSubs)
	pubStatChan = make(chan *stats, *numPubs)

	donewg.Add(*numPubs + *numSubs)

	// Run Subscribers first
	startwg.Add(*numSubs)
	for i := 0; i < *numSubs; i++ {
		subID := fmt.Sprintf("%s-sub-%d", *clientID, i)
		go runSubscriber(&startwg, &donewg, opts, *numMsgs, *ignoreOld, subID)
	}
	startwg.Wait()

	// Now Publishers
	startwg.Add(*numPubs)
	for i := 0; i < *numPubs; i++ {
		pubID := fmt.Sprintf("%s-pub-%d", *clientID, i)
		go runPublisher(&startwg, &donewg, opts, *numMsgs, *messageSize, *async, pubID, *maxPubAcks)
	}

	log.Printf("Starting benchmark [msgs=%d, pubs=%d, subs=%d]\n", *numMsgs, *numPubs, *numSubs)

	startwg.Wait()
	start := time.Now()
	donewg.Wait()
	interval := time.Since(start)
	delta := interval.Seconds()

	close(pubStatChan)
	pubStats := newStatSums()
	for s := range pubStatChan {
		pubStats.addStat(s)
	}

	close(subStatChan)
	subStats := newStatSums()
	for s := range subStatChan {
		subStats.addStat(s)
	}

	msgCount := pubStats.msgCount + subStats.msgCount
	msgThroughput := commaFormat(int64(float64(msgCount) / delta))

	fmt.Printf("STAN (Publishers/Subscribers) throughput is %s msgs/sec (%d msgs in %v)\n", msgThroughput, msgCount, interval)

	if len(pubStats.clients) > 0 {
		fmt.Printf("Publisher Stats (%d) %v\n", *numPubs, pubStats)
		if len(pubStats.clients) > 1 {
			for i, stat := range pubStats.clients {
				fmt.Printf("  [%d] %v\n", i+1, stat)
			}
			fmt.Printf("  %s\n", pubStats.minMaxAverage())
		}
	}

	if len(subStats.clients) > 0 {
		fmt.Printf("Subscriber Stats (%d) %v\n", *numSubs, subStats)
		if len(subStats.clients) > 1 {
			for i, stat := range subStats.clients {
				fmt.Printf("  [%d] %v\n", i+1, stat)
			}
			fmt.Printf("  %s\n", subStats.minMaxAverage())
		}
	}
}

func setMaxPubAcksInflight(val int) stan.Option {
	return func(o *stan.Options) error {
		o.MaxPubAcksInflight = val
		return nil
	}
}

func runPublisher(startwg, donewg *sync.WaitGroup, opts nats.Options, numMsgs int, messageSize int, async bool, pubID string, maxPubAcksInFlight int) {

	nc, err := opts.Connect()
	if err != nil {
		log.Fatalf("Can't connect: %v\n", err)
	}
	snc, err := stan.Connect("test-cluster", pubID, setMaxPubAcksInflight(maxPubAcksInFlight), stan.NatsConn(nc))
	if err != nil {
		log.Fatalf("Publisher %s can't connect: %v\n", pubID, err)
	}

	startwg.Done()

	args := flag.Args()

	subj := args[0]
	var msg []byte
	if(messageSize > 0) {
		msg = make([]byte, messageSize)
	}
	published := 0
	start := time.Now()

	if async {
		ch := make(chan bool)
		acb := func(lguid string, err error) {
			published++
			if published >= numMsgs {
				ch <- true
			}
		}
		for i := 0; i < numMsgs; i++ {
			_, err := snc.PublishAsync(subj, msg, acb)
			if err != nil {
				log.Fatal(err)
			}
		}
		<-ch
	} else {
		for i := 0; i < numMsgs; i++ {
			err := snc.Publish(subj, msg)
			if err != nil {
				log.Fatal(err)
			}
			published++
		}
	}

	pubStatChan <- newStats(start, time.Now(), nc, true)

	snc.Close()
	nc.Close()
	donewg.Done()
}

func runSubscriber(startwg, donewg *sync.WaitGroup, opts nats.Options, numMsgs int, ignoreOld bool, subID string) {
	nc, err := opts.Connect()
	if err != nil {
		log.Fatalf("Can't connect: %v\n", err)
	}
	snc, err := stan.Connect("test-cluster", subID, stan.NatsConn(nc))
	if err != nil {
		log.Fatalf("Subscriber %s can't connect: %v\n", subID, err)
	}

	args := flag.Args()
	subj := args[0]
	ch := make(chan bool)
	start := time.Now()

	received := 0
	mcb := func(msg *stan.Msg) {
		received++
		if received >= numMsgs {
			ch <- true
		}
	}

	if ignoreOld {
		snc.Subscribe(subj, mcb)
	} else {
		snc.Subscribe(subj, mcb, stan.DeliverAllAvailable())
	}
	startwg.Done()

	<-ch
	subStatChan <- newStats(start, time.Now(), nc, false)
	snc.Close()
	nc.Close()
	donewg.Done()
}

type stats struct {
	msgCount uint64
	ioBytes  uint64
	start    time.Time
	end      time.Time
}

func (s *stats) throughput() float64 {
	return float64(s.ioBytes) / s.duration().Seconds()
}

func (s *stats) rate() int64 {
	return int64(float64(s.msgCount) / s.duration().Seconds())
}

func newStats(start, end time.Time, nc *nats.Conn, isPub bool) *stats {
	s := stats{start: start, end: end}
	if isPub {
		s.msgCount = nc.OutMsgs
		s.ioBytes = nc.OutBytes
	} else {
		s.msgCount = nc.InMsgs
		s.ioBytes = nc.InBytes
	}
	return &s
}

func (s *stats) String() string {
	rate := commaFormat(s.rate())
	messages := commaFormat(int64(s.msgCount))
	throughput := humanBytes(s.throughput(), false)
	return fmt.Sprintf("%s msgs/sec | %s msgs in %v | %s/sec", rate, messages, s.duration(), throughput)
}

func (s *stats) duration() time.Duration {
	return s.end.Sub(s.start)
}

func (s *stats) Seconds() float64 {
	return s.duration().Seconds()
}

func (s *statSums) minMaxAverage() string {
	return fmt.Sprintf("min %s | avg %s | max %s | stddev %s msgs\n", commaFormat(s.minRate()), commaFormat(s.avgRate()), commaFormat(s.maxRate()), commaFormat(int64(s.stddev())))
}

func (s *statSums) minRate() int64 {
	m := int64(0)
	for i, c := range s.clients {
		if i == 0 {
			m = c.rate()
		}
		m = min(m, c.rate())
	}
	return m
}

func (s *statSums) maxRate() int64 {
	m := int64(0)
	for i, c := range s.clients {
		if i == 0 {
			m = c.rate()
		}
		m = max(m, c.rate())
	}
	return m
}

func (s *statSums) avgRate() int64 {
	sum := uint64(0)
	for _, c := range s.clients {
		sum += uint64(c.rate())
	}
	return int64(sum / uint64(len(s.clients)))
}

func (s *statSums) stddev() float64 {
	avg := float64(s.avgRate())
	sum := float64(0)
	for _, c := range s.clients {
		sum += math.Pow(float64(c.rate())-avg, 2)
	}
	variance := sum / float64(len(s.clients))
	return math.Sqrt(variance)
}

type statSums struct {
	stats
	clients []*stats
}

func newStatSums() *statSums {
	s := new(statSums)
	s.clients = make([]*stats, 0, 0)
	return s
}

func (s *statSums) addStat(e *stats) {
	s.clients = append(s.clients, e)

	if len(s.clients) == 1 {
		s.start = e.start
		s.end = e.end
	}
	s.ioBytes += e.ioBytes
	s.msgCount += e.msgCount

	if e.start.Before(s.start) {
		s.start = e.start
	}

	if e.end.After(s.end) {
		s.end = e.end
	}
}

func commaFormat(n int64) string {
	in := strconv.FormatInt(n, 10)
	out := make([]byte, len(in)+(len(in)-2+int(in[0]/'0'))/3)
	if in[0] == '-' {
		in, out[0] = in[1:], '-'
	}
	for i, j, k := len(in)-1, len(out)-1, 0; ; i, j = i-1, j-1 {
		out[j] = in[i]
		if i == 0 {
			return string(out)
		}
		if k++; k == 3 {
			j, k = j-1, 0
			out[j] = ','
		}
	}
}

func humanBytes(bytes float64, si bool) string {
	var base = 1024
	pre := [...]string{"K", "M", "G", "T", "P", "E"}
	var post = "B"
	if si {
		base = 1000
		pre = [...]string{"k", "M", "G", "T", "P", "E"}
		post = "iB"
	}
	if bytes < float64(base) {
		return fmt.Sprintf("%.2f B", bytes)
	}
	exp := int(math.Log(bytes) / math.Log(float64(base)))
	index := exp - 1
	units := pre[index] + post
	return fmt.Sprintf("%.2f %s", bytes/math.Pow(float64(base), float64(exp)), units)
}

func min(x, y int64) int64 {
	if x < y {
		return x
	}
	return y
}

func max(x, y int64) int64 {
	if x > y {
		return x
	}
	return y
}
