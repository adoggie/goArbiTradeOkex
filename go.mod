module github.com/adoggie/jaguar

go 1.18

require (
	github.com/amir-the-h/okex v1.0.31-alpha
	github.com/gookit/goutil v0.6.5
	github.com/pebbe/zmq4 v1.2.9

//go.nanomsg.org/mangos/v3 v3.4.2
)

replace github.com/amir-the-h/okex => ./pkg/okex-1.0.28-alpha

require github.com/gorilla/websocket v1.5.0 // indirect
