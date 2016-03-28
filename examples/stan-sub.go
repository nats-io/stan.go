// Copyright 2012-2016 Apcera Inc. All rights reserved.
// +build ignore

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	. "github.com/nats-io/stan"
	"github.com/nats-io/stan/pb"
)

var usageStr = `
Usage: stan-sub [options] <subject>

Options:
	-s, --server   <url>            STAN server URL(s)
	-c, --cluster  <cluster name>   STAN cluster name
	-id,--clientid <client ID>      STAN client ID

Subscription Options:
	--qgroup <name>                 Queue group
	--seq <seqno>                   Start at seqno
	--all                           Deliver all available messages
	--last                          Deliver starting with last published message
	--since <duration>              Deliver messages in last interval (e.g. 1s, 1hr)
	         (for more information: https://golang.org/pkg/time/#ParseDuration)
	--durable <name>                Durable subscriber name
`

// NOTE: Use tls scheme for TLS, e.g. stan-sub -s tls://demo.nats.io:4443 foo
func usage() {
	log.Fatalf(usageStr)
}

func printMsg(m *Msg, i int) {
	log.Printf("[#%d] Received on [%s]: '%s'\n", i, m.Subject, m)
}

func main() {
	opts := DefaultOptions

	var clusterID string
	var clientID string
	var showTime bool
	var startSeq uint64
	var startDelta string
	var deliverAll bool
	var deliverLast bool
	var durable string
	var qgroup string

	//	defaultID := fmt.Sprintf("client.%s", nuid.Next())

	flag.StringVar(&opts.NatsURL, "s", DefaultNatsURL, "The nats server URLs (separated by comma)")
	flag.StringVar(&opts.NatsURL, "server", DefaultNatsURL, "The nats server URLs (separated by comma)")
	flag.StringVar(&clusterID, "c", "test-cluster", "The STAN cluster ID")
	flag.StringVar(&clusterID, "cluster", "test-cluster", "The STAN cluster ID")
	flag.StringVar(&clientID, "id", "", "The STAN client ID to connect with")
	flag.StringVar(&clientID, "clientid", "", "The STAN client ID to connect with")
	flag.BoolVar(&showTime, "t", false, "Display timestamps")
	// Subscription options
	flag.Uint64Var(&startSeq, "seq", 0, "Start at sequence no.")
	flag.BoolVar(&deliverAll, "all", false, "Deliver all")
	flag.BoolVar(&deliverLast, "last", false, "Start with last value")
	flag.StringVar(&startDelta, "since", "", "Deliver messages since specified time offset")
	flag.StringVar(&durable, "durable", "", "Durable subscriber name")
	flag.StringVar(&durable, "qgroup", "", "Queue group name")

	log.SetFlags(0)
	flag.Usage = usage
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		usage()
	}

	sc, err := Connect(clusterID, clientID)
	if err != nil {
		log.Fatalf("Can't connect: %v\n", err)
	}
	log.Printf("Connected to %s clusterID: [%s] clientID: [%s]\n", opts.NatsURL, clusterID, clientID)

	subj, i := args[0], 0

	mcb := func(msg *Msg) {
		i++
		printMsg(msg, i)
	}

	startOpt := StartAt(pb.StartPosition_NewOnly)

	if startSeq != 0 {
		startOpt = StartAtSequence(startSeq)
	} else if deliverLast == true {
		startOpt = StartWithLastReceived()
	} else if deliverAll == true {
		log.Print("subscribing with DeliverAllAvailable")
		startOpt = DeliverAllAvailable()
	} else if startDelta != "" {
		ago, err := time.ParseDuration(startDelta)
		if err != nil {
			sc.Close()
			log.Fatal(err)
		}
		startOpt = StartAtTimeDelta(ago)
	}

	sub, err := sc.QueueSubscribe(subj, qgroup, mcb, startOpt, DurableName(durable))
	if err != nil {
		sc.Close()
		log.Fatal(err)
	}

	log.Printf("Listening on [%s], clientID=[%s], qgroup=[%s] durable=[%s]\n", subj, clientID, qgroup, durable)

	if showTime {
		log.SetFlags(log.LstdFlags)
	}

	// Wait for a SIGINT (perhaps triggered by user with CTRL-C)
	// Run cleanup when signal is received
	signalChan := make(chan os.Signal, 1)
	cleanupDone := make(chan bool)
	signal.Notify(signalChan, os.Interrupt)
	go func() {
		for _ = range signalChan {
			fmt.Println("\nReceived an interrupt, unsubscribing and closing connection...\n")
			sub.Unsubscribe()
			sc.Close()
			cleanupDone <- true
		}
	}()
	<-cleanupDone
}
