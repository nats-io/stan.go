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

	"github.com/nats-io/go-stan"
)

var usageStr = `
Usage: stan-pub [options] <subject> <message>

Options:
	-s, --server   <url>            STAN server URL(s)
	-c, --cluster  <cluster name>   STAN cluster name
	-id,--clientid <client ID>      STAN client ID
	-a, --async                     Asynchronous publish mode
`

// NOTE: Use tls scheme for TLS, e.g. stan-pub -s tls://demo.nats.io:4443 foo hello
func usage() {
	fmt.Printf("%s\n", usageStr)
	os.Exit(0)
}

func main() {
	opts := stan.DefaultOptions

	var clusterID string
	var clientID string
	var async bool

	flag.StringVar(&opts.NatsURL, "s", stan.DefaultNatsURL, "The nats server URLs (separated by comma)")
	flag.StringVar(&opts.NatsURL, "server", stan.DefaultNatsURL, "The nats server URLs (separated by comma)")
	flag.StringVar(&clusterID, "c", "test-cluster", "The STAN cluster ID")
	flag.StringVar(&clusterID, "cluster", "test-cluster", "The STAN cluster ID")
	flag.StringVar(&clientID, "id", "stan-pub", "The STAN client ID to connect with")
	flag.StringVar(&clientID, "clientid", "stan-pub", "The STAN client ID to connect with")
	flag.BoolVar(&async, "a", false, "Publish asynchronousely")
	flag.BoolVar(&async, "async", false, "Publish asynchronousely")

	log.SetFlags(0)
	flag.Usage = usage
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		usage()
	}

	sc, err := stan.Connect(clusterID, clientID)
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
			log.Fatalf("Expected a matching guid in ack callback, got %s vs %s\n", lguid, guid)
		}
		ch <- true
	}

	if async != true {
		err = sc.Publish(subj, msg)
		if err != nil {
			log.Fatalf("Error during publish: %v\n", err)
		}
		log.Printf("Published [%s] : '%s'\n", subj, msg)
	} else {
		glock.Lock()
		guid, err = sc.PublishAsync(subj, msg, acb)
		if err != nil {
			log.Fatalf("Error during async publish: %v\n", err)
		}
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
