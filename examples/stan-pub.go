// Copyright 2012-2016 Apcera Inc. All rights reserved.
// +build ignore

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/nats-io/stan"
)

var usageStr = `
Usage: stan-pub [options] <subject> <message>

Options:
	-s, --server   <url>            STAN server URL(s)
	-c, --cluster  <cluster name>   STAN cluster name
	-id,--clientid <client ID>      Unique STAN client ID
	-a, --async                     Asynchronous publish mode
`

// NOTE: Use tls scheme for TLS, e.g. stan-pub -s tls://demo.nats.io:4443 foo hello
func usage() {
	//	log.Fatalf("Usage: stan-pub [-s server (%s)] [-c cluster <cluster ID>] [-id <client ID>] <subject> <msg> \n", stan.DefaultNatsURL)
	fmt.Printf("%s\n", usageStr)
	os.Exit(0)
}

func main() {
	opts := stan.DefaultOptions

	var clusterId string
	var clientId string
	var async bool

	flag.StringVar(&opts.NatsURL, "s", stan.DefaultNatsURL, "The nats server URLs (separated by comma)")
	flag.StringVar(&opts.NatsURL, "server", stan.DefaultNatsURL, "The nats server URLs (separated by comma)")
	flag.StringVar(&clusterId, "c", "test-cluster", "The STAN cluster ID")
	flag.StringVar(&clusterId, "cluster", "", "The STAN cluster ID")
	flag.StringVar(&clientId, "id", "", "The STAN client ID to connect with")
	flag.StringVar(&clientId, "clientid", "", "The STAN client ID to connect with")
	flag.BoolVar(&async, "a", false, "Publish asynchronousely")
	flag.BoolVar(&async, "async", false, "Publish asynchronousely")

	log.SetFlags(0)
	flag.Usage = usage
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		usage()
	}

	sc, err := stan.Connect(clusterId, clientId)
	if err != nil {
		log.Fatal(err)
	}
	defer sc.Close()

	subj, msg := args[0], []byte(args[1])

	ch := make(chan bool)
	var glock sync.Mutex
	var guid string
	acb := func(lguid string, err error) {
		glock.Lock()
		log.Printf("Received ACK for guid %s\n", lguid)
		defer glock.Unlock()
		if err != nil {
			log.Fatalf("Error in server ack for guid %s: %v\n", lguid, err)
		}
		if lguid != guid {
			log.Fatal("Expected a matching guid in ack callback, got %s vs %s\n", lguid, guid)
		}
		ch <- true
	}

	if async != true {
		sc.Publish(subj, msg)
		log.Printf("Published [%s] : '%s'\n", subj, msg)
	} else {
		glock.Lock()
		guid, _ = sc.PublishAsync(subj, msg, acb)
		glock.Unlock()
		if guid == "" {
			log.Fatal("Expected non-empty guid to be returned.")
		}
		log.Printf("Published [%s] : '%s' [guid: %s]\n", subj, msg, guid)

		select {
		case <-ch:
			break
		case <-time.After(5 * time.Second):
			log.Fatal("timeout")
		}

	}
}
