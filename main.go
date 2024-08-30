package main

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"time"

	"github.com/dazeus/dazeus-go"
)

func main() {
	if len(os.Args) != 3 {
		log.Printf("Usage: %s socket carddavurl", os.Args[0])
		return
	}

	network := "kassala"
	channel := "#ru"

	socket := os.Args[1]
	carddavurl := os.Args[2]

	u, err := url.Parse(carddavurl)
	if err != nil {
		log.Fatal(err)
	}

	username := u.User.Username()
	password, _ := u.User.Password()
	u.User = nil
	endpoint := u.String()

	birthdays, err := NewBirthdays(username, password, endpoint)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	dz, err := dazeus.Connect(socket)
	if err != nil {
		log.Fatal(err)
	}

	if err := birthdays.Start(ctx); err != nil {
		log.Fatal(err)
	}

	for {
		chs, err := dz.Channels(network)
		if err != nil {
			log.Fatal(err)
		}
		var joined bool
		for _, ch := range chs {
			if ch == channel {
				joined = true
				break
			}
		}
		if joined {
			break
		}

		if err := dz.Join(network, channel); err != nil {
			log.Fatal(err)
		}

		log.Printf("Waiting until we've joined %s/%s...", network, channel)
		time.Sleep(time.Second * 5)
	}

	for {
		bs, err := birthdays.Wait(ctx)
		if err != nil {
			log.Fatal(err)
		}

		// TODO: add everyone in bs to the topic
		for _, b := range bs {
			log.Printf(b.Nick)
			dz.Message(network, channel, fmt.Sprintf("Gefeliciteerd %s! \\o/", b.Nick))
		}
	}
}
