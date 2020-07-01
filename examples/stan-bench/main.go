// Copyright 2016-2019 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/bench"

	"github.com/nats-io/stan.go"
)

// Some sane defaults
const (
	DefaultNumMsgs            = 100000
	DefaultNumPubs            = 10
	DefaultNumSubs            = 0
	DefaultSync               = false
	DefaultMessageSize        = 128
	DefaultIgnoreOld          = false
	DefaultMaxPubAcksInflight = 512
	DefaultClientID           = "benchmark"
)

func usage() {
	log.Fatalf("Usage: stan-bench [-s server (%s)] [-c CLUSTER_ID] [-id CLIENT_ID] [-qgroup QUEUE_GROUP_NAME] [-np NUM_PUBLISHERS] [-ns NUM_SUBSCRIBERS] [-n NUM_MSGS] [-ms MESSAGE_SIZE] [-csv csvfile] [-mpa MAX_NUMBER_OF_PUBLISHED_ACKS_INFLIGHT] [-io] [-sync] [--creds credentials_file] [-cd PATH_TO_CERTS] [-cf CERTIFICATE_FILE] [-ck CERTIFICATE_KEY] [-u USERID] [-pw PASSWORD] <subject>\n", nats.DefaultURL)
}

var (
	benchmark  *bench.Benchmark
	qTotalRecv int32
	qSubsLeft  int32
)

func main() {
	var clusterID string
	flag.StringVar(&clusterID, "c", "test-cluster", "The NATS Streaming cluster ID")
	flag.StringVar(&clusterID, "cluster", "test-cluster", "The NATS Streaming cluster ID")

	var urls = flag.String("s", nats.DefaultURL, "The NATS server URLs (separated by comma")
	var numPubs = flag.Int("np", DefaultNumPubs, "Number of concurrent publishers")
	var numSubs = flag.Int("ns", DefaultNumSubs, "Number of concurrent subscribers")
	var numMsgs = flag.Int("n", DefaultNumMsgs, "Number of messages to publish")
	var syncPub = flag.Bool("sync", DefaultSync, "Sync message publishing")
	var messageSize = flag.Int("ms", DefaultMessageSize, "Message size in bytes.")
	var ignoreOld = flag.Bool("io", DefaultIgnoreOld, "Subscribers ignore old messages")
	var maxPubAcks = flag.Int("mpa", DefaultMaxPubAcksInflight, "Max number of published acks in flight")
	var clientID = flag.String("id", DefaultClientID, "Benchmark process base client ID")
	var csvFile = flag.String("csv", "", "Save bench data to csv file")
	var queue = flag.String("qgroup", "", "Queue group name")
	var userCreds = flag.String("creds", "", "Credentials File")
	var certDir = flag.String("cd", "", "path to certs to load")
	var certFile = flag.String("cf", "", "certificate file")
	var certKey = flag.String("ck", "", "certificate key")
	var user = flag.String("u", "", "user id")
	var pswd = flag.String("pw", "", "password")

	log.SetFlags(0)
	flag.Usage = usage
	flag.Parse()

	args := flag.Args()
	if len(args) != 1 {
		usage()
	}

	// Setup the connect options
	opts := []nats.Option{nats.Name("NATS Streaming Benchmark")}
	// Use UserCredentials
	if *userCreds != "" {
		opts = append(opts, nats.UserCredentials(*userCreds))
	}

	// Use BasicAuth
	if len(*user) > 0 && len(*pswd) > 0 {
		opts = append(opts, nats.UserInfo(*user, *pswd))
	}

	if strings.Contains(*urls, "tls://") {
		if len(*certDir) > 0 {
			certificateFiles, _ := filepath.Glob(filepath.Join(*certDir, "*.pem"))
			opts = append(opts, nats.RootCAs(certificateFiles...))
		} else if len(*certFile) > 0 && len(*certKey) > 0 {
			opts = append(opts, nats.ClientCert(*certFile, *certKey))
		} else {
			usage()
		}
	}

	benchmark = bench.NewBenchmark("NATS Streaming", *numSubs, *numPubs)

	var startwg sync.WaitGroup
	var donewg sync.WaitGroup

	donewg.Add(*numPubs + *numSubs)

	if *queue != "" {
		qSubsLeft = int32(*numSubs)
	}
	// Run Subscribers first
	startwg.Add(*numSubs)
	for i := 0; i < *numSubs; i++ {
		subID := fmt.Sprintf("%s-sub-%d", *clientID, i)
		go runSubscriber(&startwg, &donewg, *urls, opts, clusterID, subID, *queue, *numMsgs, *messageSize, *ignoreOld)
	}
	startwg.Wait()

	// Now Publishers
	startwg.Add(*numPubs)
	pubCounts := bench.MsgsPerClient(*numMsgs, *numPubs)
	for i := 0; i < *numPubs; i++ {
		pubID := fmt.Sprintf("%s-pub-%d", *clientID, i)
		go runPublisher(&startwg, &donewg, *urls, opts, clusterID, pubCounts[i], *messageSize, *syncPub, pubID, *maxPubAcks)
	}

	log.Printf("Starting benchmark [msgs=%d, msgsize=%d, pubs=%d, subs=%d]\n", *numMsgs, *messageSize, *numPubs, *numSubs)

	startwg.Wait()
	donewg.Wait()

	benchmark.Close()
	fmt.Print(benchmark.Report())

	if len(*csvFile) > 0 {
		csv := benchmark.CSV()
		ioutil.WriteFile(*csvFile, []byte(csv), 0644)
		fmt.Printf("Saved metric data in csv file %s\n", *csvFile)
	}
}

func runPublisher(startwg, donewg *sync.WaitGroup, url string, opts []nats.Option, clusterID string, numMsgs, msgSize int, sync bool, pubID string, maxPubAcksInflight int) {
	nc, err := nats.Connect(url, opts...)
	if err != nil {
		log.Fatalf("Publisher %s can't connect: %v\n", pubID, err)
	}
	snc, err := stan.Connect(clusterID, pubID, stan.MaxPubAcksInflight(maxPubAcksInflight), stan.NatsConn(nc),
		stan.SetConnectionLostHandler(func(_ stan.Conn, reason error) {
			log.Fatalf("Connection lost, reason: %v", reason)
		}))
	if err != nil {
		log.Fatalf("Publisher %s can't connect: %v\n", pubID, err)
	}

	startwg.Done()

	args := flag.Args()

	subj := args[0]
	var msg []byte
	if msgSize > 0 {
		msg = make([]byte, msgSize)
	}
	published := 0
	start := time.Now()

	if !sync {
		ch := make(chan bool)
		acb := func(lguid string, err error) {
			if err != nil {
				log.Fatalf("Publisher %q got following error: %v", pubID, err)
			}
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

	benchmark.AddPubSample(bench.NewSample(numMsgs, msgSize, start, time.Now(), snc.NatsConn()))
	snc.Close()
	nc.Close()
	donewg.Done()
}

func runSubscriber(startwg, donewg *sync.WaitGroup, url string, opts []nats.Option, clusterID, subID, queue string, numMsgs, msgSize int, ignoreOld bool) {
	nc, err := nats.Connect(url, opts...)
	if err != nil {
		log.Fatalf("Subscriber %s can't connect: %v\n", subID, err)
	}
	snc, err := stan.Connect(clusterID, subID, stan.NatsConn(nc),
		stan.SetConnectionLostHandler(func(_ stan.Conn, reason error) {
			log.Fatalf("Connection lost, reason: %v", reason)
		}))
	if err != nil {
		log.Fatalf("Subscriber %s can't connect: %v\n", subID, err)
	}

	args := flag.Args()
	subj := args[0]
	ch := make(chan time.Time, 2)

	isQueue := queue != ""
	received := 0
	mcb := func(msg *stan.Msg) {
		received++
		if received == 1 {
			ch <- time.Now()
		}
		if isQueue {
			if atomic.AddInt32(&qTotalRecv, 1) >= int32(numMsgs) {
				ch <- time.Now()
			}
		} else {
			if received >= numMsgs {
				ch <- time.Now()
			}
		}
	}

	var sub stan.Subscription
	if ignoreOld {
		sub, err = snc.QueueSubscribe(subj, queue, mcb)
	} else {
		sub, err = snc.QueueSubscribe(subj, queue, mcb, stan.DeliverAllAvailable())
	}
	if err != nil {
		log.Fatalf("Subscriber %s can't subscribe: %v", subID, err)
	}
	startwg.Done()

	start := <-ch
	end := <-ch
	benchmark.AddSubSample(bench.NewSample(received, msgSize, start, end, snc.NatsConn()))
	// For queues, since not each member receives the total number of messages,
	// when a member is done, it needs to publish a message to unblock other member(s).
	if isQueue {
		if sr := atomic.AddInt32(&qSubsLeft, -1); sr > 0 {
			// Close this queue member first so that there is no chance that the
			// server sends the message we are going to publish back to this member.
			sub.Close()
			snc.Publish(subj, []byte("done"))
		}
	}
	snc.Close()
	nc.Close()
	donewg.Done()
}
