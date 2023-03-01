module goofy

go 1.18

require github.com/adoggie/jaguar v0.0.0-00010101000000-000000000000

replace github.com/adoggie/jaguar => ../../

require (
	github.com/amir-the-h/okex v1.0.31-alpha // indirect
	github.com/gookit/goutil v0.6.5 // indirect
	github.com/gorilla/websocket v1.5.0 // indirect
	github.com/pebbe/zmq4 v1.2.9 // indirect
)

replace github.com/amir-the-h/okex => ../../pkg/okex-1.0.28-alpha
