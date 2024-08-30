package main

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/emersion/go-vcard"
	webdav "github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/carddav"
)

type Birthday struct {
	Nick string
}

type Birthdays struct {
	username string
	client   *carddav.Client

	mtx                sync.Mutex
	birthdays          map[string][]Birthday
	updated            chan struct{}
	today              string
	congratulatedToday map[string]struct{}
}

func NewBirthdays(username, password, endpoint string) (*Birthdays, error) {
	hc := webdav.HTTPClientWithBasicAuth(nil, username, password)
	cc, err := carddav.NewClient(hc, endpoint)
	if err != nil {
		return nil, err
	}
	return &Birthdays{
		username: username,
		client:   cc,
	}, nil
}

func (b *Birthdays) Start(ctx context.Context) error {
	b.updated = make(chan struct{})
	if err := b.refresh(ctx); err != nil {
		return err
	}
	go func() {
		// Refresh CardDav every hour.
		for {
			select {
			case <-time.After(time.Hour):
				if err := b.refresh(ctx); err != nil {
					log.Print(err)
				}
			case <-ctx.Done():
				close(b.updated)
				return
			}
		}
	}()
	return nil
}

func (b *Birthdays) Wait(ctx context.Context) ([]Birthday, error) {
	amsterdam, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		return nil, err
	}

	for ctx.Err() == nil {
		// Check if anyone hasn't been congratulated today, yet.
		now := time.Now()
		today := now.Format("0102")
		b.mtx.Lock()
		if b.today != today {
			b.today = today
			b.congratulatedToday = map[string]struct{}{}
		}

		birthdaysToday := b.birthdays[today]

		log.Printf("It's %s. %d people have their birthday today. %d people have already been congratulated.",
			today, len(birthdaysToday), len(b.congratulatedToday))

		// If anyone hasn't been congratulated yet, return the full list.
		var everyoneCongratulated = true
		for _, bt := range birthdaysToday {
			if _, already := b.congratulatedToday[bt.Nick]; !already {
				b.congratulatedToday[bt.Nick] = struct{}{}
				everyoneCongratulated = false
			}
		}

		b.mtx.Unlock()

		if !everyoneCongratulated {
			return birthdaysToday, nil
		}

		// Everybody's been congratulated. Try again on the next refresh,
		// the next day, or stop when the context is done.
		nextDay := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, amsterdam)
		duration := time.Until(nextDay)

		log.Printf("Everyone's been congratulated. Waiting %v seconds until next day...", duration.Seconds())

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(duration):
			continue
		case <-b.updated:
			continue
		}
	}
	return nil, ctx.Err()
}

func (b *Birthdays) refresh(ctx context.Context) error {
	log.Printf("Refreshing birthdays...")
	principal, err := b.client.FindCurrentUserPrincipal(ctx)
	if err != nil {
		return err
	}
	homeSet, err := b.client.FindAddressBookHomeSet(ctx, principal)
	if err != nil {
		return err
	}
	addrBooks, err := b.client.FindAddressBooks(ctx, homeSet)
	if err != nil {
		return err
	}

	birthdays := map[string][]Birthday{}

	for _, book := range addrBooks {
		addresses, err := b.client.QueryAddressBook(ctx, book.Path, &carddav.AddressBookQuery{
			DataRequest: carddav.AddressDataRequest{
				Props:   []string{vcard.FieldBirthday, vcard.FieldNickname, vcard.FieldName},
				AllProp: true,
			},
		})
		if err != nil {
			return err
		}
		for _, addr := range addresses {
			name := addr.Card.Name()
			var nick string
			if n := addr.Card.Value(vcard.FieldNickname); n != "" {
				nick = n
			} else if name != nil && name.GivenName != "" {
				nick = name.GivenName
			} else {
				log.Printf("Unknown nick for card: %v (%s / %s)", addr, addr.Card.Value(vcard.FieldBirthday), addr.Card.Value(vcard.FieldEmail))
				continue
			}

			birthday := addr.Card.Value(vcard.FieldBirthday)
			if birthday == "" {
				log.Printf("Unknown birthday for nick: %s", nick)
			}

			birthday = birthday[4:]

			birthdays[birthday] = append(birthdays[birthday], Birthday{
				Nick: nick,
			})
			log.Printf("%s: %s", birthday, nick)
		}
	}

	b.mtx.Lock()
	defer b.mtx.Unlock()
	b.birthdays = birthdays
	select {
	case b.updated <- struct{}{}:
	default:
	}
	return nil
}
