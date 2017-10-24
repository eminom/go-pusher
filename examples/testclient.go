package main

// Display on console live Trades and Orders book from Bitstamp
// run with
// 		go run client.go

import (
	"github.com/eminom/go-pusher"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	APP_KEY = "de504dc5763aeef9ff52" // bitstamp
)

func testProcedure() (quitReason int) {
	defer func() {
		if err := recover(); err != nil {
			log.Printf("%v", err)
			quitReason = 0
		}
	}()

	log.Println("init...")
	pusherClient, err := pusher.NewClient(APP_KEY)
	// if you need to connect to custom endpoint
	// pusherClient, err := pusher.NewCustomClient(APP_KEY, "localhost:8080", "ws")
	if err != nil {
		log.Fatalln(err)
	}
	// Subscribe
	err = pusherClient.Subscribe("live_trades")
	if err != nil {
		log.Println("Subscription error : ", err)
	}

	log.Println("first subscribe done")

	// err = pusherClient.Subscribe("order_book")
	// if err != nil {
	// 	log.Println("Subscription error : ", err)
	// }

	// test subcride to and already subscribed channel
	// Uncomment these lines to subscribe order_book.
	// err = pusherClient.Subscribe("order_book")
	// if err != nil {
	// 	log.Println("Subscription error : ", err)
	// }

	err = pusherClient.Subscribe("foo")
	if err != nil {
		log.Println("Subscription error : ", err)
	}
	log.Println("Subscribed to foo")

	err = pusherClient.Unsubscribe("foo")
	if err != nil {
		log.Println("Unsubscription error : ", err)
	}
	log.Println("Unsubscibed from foo")

	// Bind events
	dataChannelTrade, err := pusherClient.Bind("data")
	if err != nil {
		log.Println("Bind error: ", err)
	}
	log.Println("Binded to 'data' event")
	tradeChannelTrade, err := pusherClient.Bind("trade")
	if err != nil {
		log.Println("Bind error: ", err)
	}
	log.Println("Binded to 'trade' event")

	// Test bind/unbind
	_, err = pusherClient.Bind("foo")
	if err != nil {
		log.Println("Bind error: ", err)
	}
	pusherClient.Unbind("foo")

	log.Println("init done")

	closCh := make(chan os.Signal, 1)
	defer close(closCh)
	signal.Notify(closCh, os.Kill, os.Interrupt, syscall.SIGTERM)
	func() {
		for {
			select {
			case <-closCh:
				log.Println("closed by user")
				return
			case dataEvt, ok := <-dataChannelTrade:
				if ok {
					log.Println("ORDER BOOK: " + dataEvt.Data)
				}
			case tradeEvt, ok := <-tradeChannelTrade:
				if ok {
					log.Println("TRADE: " + tradeEvt.Data)
				}
			}
		}
	}()
	quitReason = 1
	pusherClient.Close()
	return
}

func main() {
	reconCount := 0
	for testProcedure() != 1 {
		log.Println("need to break por un momento")
		<-time.After(2 * time.Second)
		log.Println("reconnecting...")
		reconCount++
	}
	log.Printf("reconnect for %v\n", reconCount)
}
