package internal

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

type ResilientWriter struct {
	w io.Writer
}

func (r *ResilientWriter) Write(p []byte) (int, error) {
	remaining := len(p)
	for {
		n, err := r.w.Write(p)
		if err != nil {
			return 0, err
		}
		remaining -= n
		if remaining == 0 {
			return len(p), nil
		}
		log.Printf("Short write: n=%d, remaining=%d", n, remaining)
		p = p[n:]
	}
}

type ProxyConnectionHandler struct {
	address     string
	failures    int64
	flushTicker *time.Ticker
	done        chan bool
	mtx         sync.RWMutex
	conn        net.Conn
	writer      *bufio.Writer
}

func NewProxyConnectionHandler(address string, ticker *time.Ticker) ConnectionHandler {
	return &ProxyConnectionHandler{
		address:     address,
		flushTicker: ticker,
	}
}

func (handler *ProxyConnectionHandler) Start() {
	handler.done = make(chan bool)

	go func() {
		for {
			select {
			case <-handler.flushTicker.C:
				err := handler.Flush()
				if err != nil {
					log.Println(err)
				}
			case <-handler.done:
				return
			}
		}
	}()
}

func (handler *ProxyConnectionHandler) Connect() error {
	handler.mtx.Lock()
	defer handler.mtx.Unlock()

	var err error
	handler.conn, err = net.DialTimeout("tcp", handler.address, time.Second*10)
	if err != nil {
		handler.conn = nil
		return fmt.Errorf("unable to connect to Wavefront proxy at address: %s, err: %q", handler.address, err)
	}
	log.Printf("connected to Wavefront proxy at address: %s", handler.address)
	handler.writer = bufio.NewWriter(&ResilientWriter{w: handler.conn})
	return nil
}

func (handler *ProxyConnectionHandler) Connected() bool {
	handler.mtx.RLock()
	defer handler.mtx.RUnlock()
	return handler.conn != nil
}

func (handler *ProxyConnectionHandler) Close() {
	err := handler.Flush()
	if err != nil {
		log.Println(err)
	}

	close(handler.done)
	handler.flushTicker.Stop()

	handler.mtx.RLock()
	defer handler.mtx.RUnlock()

	if handler.conn != nil {
		handler.conn.Close()
		handler.conn = nil
		handler.writer = nil
	}
}

func (handler *ProxyConnectionHandler) Flush() error {
	handler.mtx.RLock()
	handler.mtx.RUnlock()

	if handler.writer != nil {
		err := handler.writer.Flush()
		if err != nil {
			handler.resetConnection()
		}
		return err
	}
	return nil
}

func (handler *ProxyConnectionHandler) GetFailureCount() int64 {
	return atomic.LoadInt64(&handler.failures)
}

func (handler *ProxyConnectionHandler) SendData(lines string) error {
	// if the connection was closed or interrupted - don't cause a panic (we'll retry at next interval)
	defer func() {
		if r := recover(); r != nil {
			// we couldn't write the line so something is wrong with the connection
			log.Println("error sending data", r)
			handler.resetConnection()
		}
	}()

	handler.mtx.RLock()
	defer handler.mtx.RUnlock()

	if handler.conn != nil {
		_, err := fmt.Fprint(handler.writer, lines)
		if err != nil {
			atomic.AddInt64(&handler.failures, 1)
		}
		return err
	}
	return fmt.Errorf("failed to send data: invalid wavefront proxy connection")
}

func (handler *ProxyConnectionHandler) resetConnection() {
	log.Println("resetting wavefront proxy connection")
	handler.mtx.Lock()
	defer handler.mtx.Unlock()
	handler.conn = nil
	handler.writer = nil
}
