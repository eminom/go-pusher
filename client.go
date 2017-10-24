package pusher

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
	"log"
	"net/http"
	"sync"
	"time"
)

const (
	_MAX_CLOSE_DELAY = 10
)

type Client struct {
	ws                 *websocket.Conn
	haltCh             chan bool
	subscribedChannels *subscribedChannels
	binders            map[string]chan *Event
	doneCh             chan struct{}
	rwMutex            sync.Mutex
	waitGroup          *sync.WaitGroup
}

func (c *Client) writeMsg(messageType int, data []byte) error {
	defer c.rwMutex.Unlock()
	c.rwMutex.Lock()
	return c.ws.WriteMessage(messageType, data)
}

// heartbeat send a ping frame to server each - TODO reconnect on disconnect
func (c *Client) heartbeat() {
	defer c.waitGroup.Done()
	defer func() {
		log.Println("heartbeat() is quitting")
	}()
	for !c.isHalted() {
		log.Println("go to heart-beat")
		// websocket.Message.Send(c.ws, `{"event":"pusher:ping","data":"{}"}`)
		select {
		case <-time.After(HEARTBEAT_RATE * time.Second):
		case <-c.haltCh:
			log.Println("break in sleep")
			return
		}
		c.writeMsg(websocket.PingMessage, []byte(`{"event":"pusher:ping","data":"{}"}`))
	}
}

// listen to Pusher server and process/dispatch recieved events
func (c *Client) listen() {
	defer c.waitGroup.Done()
	defer func() {
		close(c.doneCh)
		log.Println("listen() is quitting")
	}()
	for !c.isHalted() {
		var event Event
		//err := websocket.JSON.Receive(c.ws, &event)
		_, message, err := c.ws.ReadMessage()
		if err != nil {
			if c.isHalted() {
				// Normal termination (ws Receive returns error when ws is
				// closed by other goroutine)
				return
			}
			log.Println("Listen error : ", err)
			panic(err)
		} else {
			//log.Println(event)
			if err := json.Unmarshal(message, &event); err != nil {
				panic(err)
			}
			switch event.Event {
			case "pusher:ping":
				//websocket.Message.Send(c.ws, `{"event":"pusher:pong","data":"{}"}`)
				c.writeMsg(websocket.PongMessage, []byte(`{"event":"pusher:pong","data":"{}"}`))
			case "pusher:pong":
				// nunca
			case "pusher:error":
				log.Println("Event error received: ", event.Data)
			default:
				if bindedCh, ok := c.binders[event.Event]; ok {
					bindedCh <- &event
				}
			}
		}
	}
}

// Subsribe to a channel
func (c *Client) Subscribe(channel string) (err error) {
	// Already subscribed ?
	if c.subscribedChannels.contains(channel) {
		err = errors.New(fmt.Sprintf("Channel %s already subscribed", channel))
		return
	}
	// err = websocket.Message.Send(c.ws, fmt.Sprintf(`{"event":"pusher:subscribe","data":{"channel":"%s"}}`, channel))
	c.writeMsg(websocket.TextMessage, []byte(fmt.Sprintf(`{"event":"pusher:subscribe","data":{"channel":"%s"}}`, channel)))
	if err != nil {
		return
	}
	err = c.subscribedChannels.add(channel)
	return
}

// Unsubscribe from a channel
func (c *Client) Unsubscribe(channel string) (err error) {
	// subscribed ?
	if !c.subscribedChannels.contains(channel) {
		err = errors.New(fmt.Sprintf("Client isn't subscrived to %s", channel))
		return
	}
	// err = websocket.Message.Send(c.ws, fmt.Sprintf(`{"event":"pusher:unsubscribe","data":{"channel":"%s"}}`, channel))
	c.writeMsg(websocket.TextMessage, []byte(fmt.Sprintf(`{"event":"pusher:unsubscribe","data":{"channel":"%s"}}`, channel)))
	if err != nil {
		return
	}
	// Remove channel from subscribedChannels slice
	c.subscribedChannels.remove(channel)
	return
}

// Bind an event
func (c *Client) Bind(evt string) (dataChannel chan *Event, err error) {
	// Already binded
	if _, ok := c.binders[evt]; ok {
		err = errors.New(fmt.Sprintf("Event %s already binded", evt))
		return
	}
	// New data channel
	dataChannel = make(chan *Event, EVENT_CHANNEL_BUFF_SIZE)
	c.binders[evt] = dataChannel
	return
}

// Unbind a event
func (c *Client) Unbind(evt string) {
	delete(c.binders, evt)
}

func NewCustomClient(appKey, host, scheme string) (*Client, error) {
	origin := "http://localhost/"
	url := scheme + "://" + host + "/app/" + appKey + "?protocol=" + PROTOCOL_VERSION
	//ws, err := websocket.Dial(url, "", origin)
	ws, _, err := websocket.DefaultDialer.Dial(url, http.Header{
		"Origin": []string{origin},
	})
	if err != nil {
		return nil, err
	}
	//var resp = make([]byte, 11000) // Pusher max message size is 10KB
	_, resp, err := ws.ReadMessage()
	if err != nil {
		return nil, err
	}
	var eventStub EventStub
	err = json.Unmarshal(resp, &eventStub)
	if err != nil {
		return nil, err
	}
	switch eventStub.Event {
	case "pusher:error":
		var ewe EventError
		if err = json.Unmarshal(resp, &ewe); err != nil {
			return nil, err
		}
		return nil, ewe
	case "pusher:connection_established":
		sChannels := new(subscribedChannels)
		sChannels.channels = make([]string, 0)
		pClient := Client{
			ws:                 ws,
			haltCh:             make(chan bool),
			subscribedChannels: sChannels,
			binders:            make(map[string]chan *Event),
			doneCh:             make(chan struct{}, 1),
			waitGroup:          new(sync.WaitGroup),
		}
		pClient.waitGroup.Add(2)
		go pClient.heartbeat()
		go pClient.listen()
		return &pClient, nil
	}
	return nil, errors.New("Ooooops something wrong happen")
}

// NewClient initialize & return a Pusher client
func NewClient(appKey string) (*Client, error) {
	return NewCustomClient(appKey, "ws.pusherapp.com:443", "wss")
}

// Stopped checks, in a non-blocking way, if client has been closed.
func (c *Client) isHalted() bool {
	select {
	case <-c.haltCh:
		return true
	default:
		return false
	}
}

// Close the underlying Pusher connection (websocket)
func (c *Client) Close() error {
	// Closing the c.haltCh channel "broadcasts" the stop signal.
	close(c.haltCh)
	if err := c.writeMsg(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
	); err != nil {
		return err
	}
	select {
	case <-time.After(_MAX_CLOSE_DELAY * time.Second):
		log.Println("close timeout")
	case <-c.doneCh:
		//log.Println("channel done")
	}
	//log.Println("waiting for all subs to quit")
	c.waitGroup.Wait()
	for _, evtCh := range c.binders {
		close(evtCh)
	}
	return c.ws.Close()
}
