package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true }, // allow any origin for demo
}

type Msg struct {
	From    string `json:"from"`
	Content string `json:"content"`
}

func echo(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil) // handshake
	if err != nil {
		log.Print("upgrade:", err)
		return
	}
	defer conn.Close()

	for { // read loop
		mt, msg, err := conn.ReadMessage() // mt = text/binary
		if err != nil {
			log.Println("read:", err)
			break
		}
		fmt.Printf("recv: %s\n", msg)
		if err = conn.WriteMessage(mt, msg); err != nil { // echo back
			log.Println("write:", err)
			break
		}
	}
}

func echoJSON(w http.ResponseWriter, r *http.Request) {
	upgrader.Subprotocols = []string{"json"} // 1. tell client we speak json
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}
	defer conn.Close()

	for {
		var m Msg
		// 2. decode JSON text frame into Go value
		if err := conn.ReadJSON(&m); err != nil {
			log.Println("read:", err)
			break
		}
		log.Printf("recv: %+v", m)

		// 3. echo it back (could mutate here)
		if err := conn.WriteJSON(m); err != nil {
			log.Println("write:", err)
			break
		}
	}
}

func main() {
	http.HandleFunc("/ws", echoJSON) // endpoint
	log.Fatal(http.ListenAndServe(":8080", nil))
}
