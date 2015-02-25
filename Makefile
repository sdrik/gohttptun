all:
	cd client-tcp-over-http; go build -o client-tcp-over-http
	cd server-tcp-over-http; go build -o server-tcp-over-http

debug:
	cd client; go build  -gcflags "-N -l" -o client-tcp-over-http
	cd server; go build  -gcflags "-N -l" -o server-tcp-over-http
