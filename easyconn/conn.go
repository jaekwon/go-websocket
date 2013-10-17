// Basically the examples/chat/conn.go, but modified for more general use.

package websocket

import (
	"github.com/jaekwon/go-websocket/websocket"
	"io/ioutil"
	"log"
	"net/http"
	"time"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10
)

// connection is an middleman between the websocket connection and the hub.
type Connection struct {
	// The websocket connection.
	WS *websocket.Conn

	// Buffered channel of outbound messages.
	Send chan []byte
}

type ConnMessage struct {
	Connection *Connection
	Message    []byte
}

// readPump pumps messages from the websocket connection to the hub.
func (c *Connection) readPump(onClose chan *Connection, onMessage chan ConnMessage, maxMessageSize int64) {
	defer func() {
		onClose <- c
		c.WS.Close()
	}()
	c.WS.SetReadLimit(maxMessageSize)
	c.WS.SetReadDeadline(time.Now().Add(pongWait))
	for {
		mt, r, err := c.WS.NextReader()
		if err != nil {
			break
		}
		switch mt {
		case websocket.PingMessage:
			c.WS.SetReadDeadline(time.Now().Add(pongWait))
		case websocket.TextMessage:
			message, err := ioutil.ReadAll(r)
			if err != nil {
				break
			}
			onMessage <- ConnMessage{c, message}
		}
	}
}

// write writes a message with the given message type and payload.
func (c *Connection) write(mt int, payload []byte) error {
	c.WS.SetWriteDeadline(time.Now().Add(writeWait))
	return c.WS.WriteMessage(mt, payload)
}

// writePump pumps messages from the hub to the websocket connection.
func (c *Connection) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.WS.Close()
	}()
	for {
		select {
		case message, ok := <-c.Send:
			if !ok {
				c.write(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.write(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			if err := c.write(websocket.PingMessage, []byte{}); err != nil {
				return
			}
		}
	}
}

func Upgrade(w http.ResponseWriter, r *http.Request, onClose chan *Connection, onMessage chan ConnMessage, maxMessageSize int64) *Connection {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", 405)
		return nil
	}
	if r.Header.Get("Origin") != "http://"+r.Host {
		http.Error(w, "Origin not allowed", 403)
		return nil
	}
	ws, err := websocket.Upgrade(w, r.Header, nil, 1024, 1024)
	if _, ok := err.(websocket.HandshakeError); ok {
		http.Error(w, "Not a websocket handshake", 400)
		return nil
	} else if err != nil {
		log.Println(err)
		return nil
	}
	c := &Connection{Send: make(chan []byte, 256), WS: ws}
	go c.writePump()
	go c.readPump(onClose, onMessage, maxMessageSize)
	return c
}
