// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

// hellobot is the smallest complete ConfigHub bot: a process that claims a Channel, handles
// two commands, and logs the events the Channel subscribes to. It exists to be read and
// copied. Everything it does against ConfigHub goes through the SDK's channel listener.
//
//	CONFIGHUB_URL=https://hub.confighub.com \
//	CONFIGHUB_AUTH_PRIVATE_KEY="$(cat ~/.confighub/keys/hellobot.jwk)" \
//	HELLOBOT_CHANNEL=platform/hellobot \
//	hellobot
//
// hellobot --print-commands prints the commands it handles, as the JSON that
// cub channel create --commands takes.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/confighub/sdk/core/channel"
	"github.com/confighub/sdk/core/cubapi"
	goclientnew "github.com/confighub/sdk/core/openapi/goclient-new"
)

// commands is what hellobot handles, as the Channel has to declare it. A bot prints this and
// an operator creates the Channel with it; the listener refuses a Channel that declares
// anything else, so the declaration is a contract visible before the process runs.
var commands = map[string]goclientnew.CommandSignature{
	"hello.say": {
		Description: "Say hello to someone.",
		Options: []goclientnew.CommandOption{
			{Name: "name", DataType: "string", Required: true, Description: "who to greet"},
			{Name: "shout", DataType: "bool", Description: "in capitals"},
		},
	},
	"hello.wait": {
		Description: "Take a while, to see a run in flight.",
		Options: []goclientnew.CommandOption{
			{Name: "seconds", DataType: "int", Required: true, Description: "how long, at most 25: a run has 30 seconds to answer"},
		},
	},
}

func main() {
	printCommands := flag.Bool("print-commands", false, "print the commands hellobot handles, as JSON for cub channel create --commands, and exit")
	flag.Parse()
	if *printCommands {
		encoded, _ := json.MarshalIndent(commands, "", "  ")
		fmt.Println(string(encoded))
		return
	}

	server := strings.TrimRight(os.Getenv("CONFIGHUB_URL"), "/")
	key := os.Getenv("CONFIGHUB_AUTH_PRIVATE_KEY")
	ref := os.Getenv("HELLOBOT_CHANNEL")
	if server == "" || key == "" || ref == "" {
		log.Fatal("set CONFIGHUB_URL, CONFIGHUB_AUTH_PRIVATE_KEY (the service account's private JWK) and HELLOBOT_CHANNEL (<space>/<channel>)")
	}

	// The service account's key signs a short-lived assertion for a session, and the client
	// renews the session for as long as the process runs.
	signer, err := cubapi.NewAssertionSigner([]byte(key), "", "")
	if err != nil {
		log.Fatalf("private key: %v", err)
	}
	client, err := cubapi.NewClient(cubapi.ClientOptions{
		ServerURL:     server,
		Authenticator: cubapi.AssertionAuthenticator(server, signer),
		UserAgent:     "hellobot",
	})
	if err != nil {
		log.Fatal(err)
	}

	var greetings atomic.Int64
	listener := channel.NewListener(client, cubapi.ParseRef(ref), channel.Options{Concurrency: 4}).
		// A handler reads the run's options, does its work, and returns a result or an error.
		// The result is any JSON; an error makes the run Failed with its message.
		Handle("hello.say", func(ctx context.Context, run *channel.Run) (json.RawMessage, error) {
			name := run.String("name")
			if name == "" {
				// Reject says the run was not this bot's to do, as opposed to tried and failed.
				return nil, channel.Reject("name is empty")
			}
			greeting := "Hello, " + name + "!"
			if run.Bool("shout") {
				greeting = strings.ToUpper(greeting)
			}
			n := greetings.Add(1)
			log.Printf("hello.say: %s (greeting %d)", greeting, n)
			return json.Marshal(map[string]any{"greeting": greeting, "count": n})
		}).
		// The handler's context ends when the run's answer is overdue or the bot is shutting
		// down, so work that waits should wait on it.
		Handle("hello.wait", func(ctx context.Context, run *channel.Run) (json.RawMessage, error) {
			seconds := run.Int("seconds")
			if seconds < 0 || seconds > 25 {
				return nil, fmt.Errorf("seconds must be between 0 and 25, got %d", seconds)
			}
			select {
			case <-time.After(time.Duration(seconds) * time.Second):
				return json.Marshal(map[string]any{"waited": seconds})
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}).
		// Events the Channel subscribes to arrive over the same poll. Delivery is best effort:
		// react, and reconcile from state elsewhere if it matters.
		OnEvent(func(ctx context.Context, event *goclientnew.EventLog) {
			space := "no space"
			if event.SpaceID != nil {
				space = event.SpaceID.String()
			}
			log.Printf("event %s about space %s (position %d)", event.EventType, space, event.CursorID)
		})

	// A readiness probe, when asked for: whether the lease is held and when the bot last polled.
	if port := os.Getenv("HELLOBOT_HTTP_PORT"); port != "" {
		go serveProbes(port, listener)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	log.Printf("hellobot: listening on channel %s at %s, handling %s", ref, server, strings.Join(listener.Commands(), ", "))
	if err := listener.Run(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("hellobot: %v", err)
	}
	log.Println("hellobot: stopped")
}

// serveProbes answers /healthz while the process runs and /readyz while it holds the lease.
func serveProbes(port string, listener *channel.Listener) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		status := listener.Status()
		if !status.LeaseHeld {
			http.Error(w, "lease not held", http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(status)
	})
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Printf("hellobot: probes: %v", err)
	}
}
