package http

//
// Http server library.
//
// Support concurrent and keep-alive http requests.
// Not support: chuck transfer encoding.
//
// Note:
// * Server use keep-alive http connections regardless of
//   "Connection: keep-alive" header.
// * Content-Length and Host headers are necessary in requests.
// * Content-Length header is necessary in responses.
// * Header value is single.
// * Request-URI must be absolute path. Like: "/add", "/incr".

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

// Server here resembles ServeMux in golang standard lib.
// Refer to https://golang.org/pkg/net/http/#ServeMux.
type Server struct {
	Addr     string
	l        net.Listener
	mu       sync.Mutex
	doneChan chan struct{}

	// Your data here.
	handlers   map[string]Handler
	activeConn map[*httpConn]struct{}
}

// NewServer initilizes the server of the speficif host.
// The host param includes the hostname and port.
func NewServer(host string) (s *Server) {
	srv := &Server{Addr: host}
	srv.doneChan = make(chan struct{})

	// Your initialization code here.
	srv.handlers = make(map[string]Handler)
	srv.activeConn = make(map[*httpConn]struct{})

	return srv
}

// Handler process the HTTP request and get the response.
//
// Handler should not modify the request.
type Handler interface {
	ServeHTTP(resp *Response, req *Request)
}

// A HandlerFunc responds to an HTTP request.
// Behave the same as he Handler.
type HandlerFunc func(resp *Response, req *Request)

// ServeHTTP calls f(w, r).
func (f HandlerFunc) ServeHTTP(w *Response, r *Request) {
	f(w, r)
}

// NotFoundHandler gives 404 with the blank content.
var NotFoundHandler HandlerFunc = func(resp *Response, req *Request) {
	resp.Write([]byte{})
	resp.WriteStatus(StatusNotFound)
}

// AddHandlerFunc add handlerFunc to the list of handlers.
func (srv *Server) AddHandlerFunc(pattern string, handlerFunc HandlerFunc) {
	srv.AddHandler(pattern, handlerFunc)
}

// AddHandler add handler to the list of handlers.
//
// "" pattern or nil handler is forbidden.
func (srv *Server) AddHandler(pattern string, handler Handler) {
	if pattern == "" {
		panic("http: invalid pattern " + pattern)
	}
	if handler == nil {
		panic("http: nil handler")
	}

	// TODO
	srv.handlers[pattern] = handler
	// fmt.Println("Add handler: " + pattern)
}

// Find a handler matching the path using most-specific
// (longest) matching. If no handler matches, return
// the NotFoundHandler.
func (srv *Server) match(path string) (h Handler) {
	// TODO
	maxLen := 0
	for p, tmpH := range srv.handlers {
		if pathMatch(p, path) && len(p) > maxLen {
			maxLen = len(p)
			h = tmpH
		}
	}
	if h != nil {
		return h
	}
	return NotFoundHandler
}

// Does path match pattern?
// "/" matches path: "/*"
// "/cart/" matches path: "/cart/*"
// "/login" only matches path: "/login"
func pathMatch(pattern, path string) bool {
	if len(pattern) == 0 {
		// should not happen
		return false
	}
	n := len(pattern)
	if pattern[n-1] != '/' {
		return pattern == path
	}
	return len(path) >= n && path[0:n] == pattern
}

// Close immediately closes active net.Listener and any
// active http connections.
//
// Close returns any error returned from closing the Server's
// underlying Listener.
func (srv *Server) Close() (err error) {
	srv.mu.Lock()
	defer srv.mu.Unlock()

	select {
	case <-srv.doneChan:
		// Already closed. Don't close again.
	default:
		// Safe to close here. We're the only closer, guarded
		// by s.mu.
		close(srv.doneChan)
	}
	err = srv.l.Close()

	// TODO

	return
}

// ErrServerClosed is returned by the Server's Serve, ListenAndServe,
// and ListenAndServeTLS methods after a call to Shutdown or Close.
var ErrServerClosed = errors.New("http: Server closed")

// ListenAndServe start listening and serve http connections.
// The method won't return until someone closes the server.
func (srv *Server) ListenAndServe() (err error) {
	// listen on the specific tcp addr, then call Serve()
	l, err := net.Listen("tcp", srv.Addr)
	defer l.Close()
	if err != nil {
		return
	}
	srv.l = l
	fmt.Println("start listening...")

	// TODO
	for {
		conn, err := l.Accept()
		if err != nil {
			// server is closed
			select {
			case <-srv.doneChan:
				return ErrServerClosed
			default:
			}
			// other errors
			return err
		}
		hc := srv.newConn(conn)
		srv.mu.Lock()
		srv.activeConn[hc] = struct{}{}
		srv.mu.Unlock()
		go hc.serve()
	}
}

func (srv *Server) newConn(conn net.Conn) *httpConn {
	return &httpConn{srv: srv, rw: conn.(*net.TCPConn)}
}

// A httpConn represents an HTTP connection in the server side.
type httpConn struct {
	srv *Server
	rw  io.ReadWriter
}

// Serve a new connection.
func (hc *httpConn) serve() {
	// Server the http connection in loop way until something goes wrong.
	// The http connection will be closed in the case of errors.
	for {
		// Construct the request from the TCP stream.
		req, err := hc.constructReq()
		if err != nil {
			hc.close()
			return
		}
		resp := &Response{Proto: HTTPVersion, Header: make(map[string]string)}

		// Find the matched handler.
		handler := hc.srv.match(req.URL.Path)

		// Handler it in user-defined logics or NotFoundHandler.
		handler.ServeHTTP(resp, req)

		// The response must contain HeaderContentLength in its Header.
		resp.Header[HeaderContentLength] = strconv.FormatInt(resp.ContentLength, 10)

		// *** Discard rest of request body.
		io.Copy(ioutil.Discard, req.Body)

		// Write the response to the TCP stream.
		err = hc.writeResp(resp)
		if err != nil {
			hc.close()
			return
		}
	}
}

// Close the http connection.
func (hc *httpConn) close() {
	// TODO
	hc.srv.mu.Lock()
	hc.rw.(*net.TCPConn).Close()
	delete(hc.srv.activeConn, hc)
	hc.srv.mu.Unlock()
}

// Write the response to the TCP stream.
//
// If TCP errors occur, err is not nil.
func (hc *httpConn) writeResp(resp *Response) (err error) {
	// TODO
	writer := bufio.NewWriterSize(hc.rw, ServerResponseBufSize)
	// write status-line
	statusLine := resp.Proto + " " + strconv.Itoa(resp.StatusCode) + " " + resp.Status
	_, err = writer.WriteString(statusLine)
	if err != nil {
		return err
	}
	// write headers
	for k, v := range resp.Header {
		h := k + ": " + v
		_, err = writer.WriteString(h)
		if err != nil {
			return err
		}
	}
	// write CRLF
	err = writer.WriteByte('\n')
	if err != nil {
		return err
	}
	// writer body
	_, err = writer.Write(resp.writeBuff[:resp.ContentLength])
	if err != nil {
		return err
	}
	err = writer.Flush()
	if err != nil {
		return
	}
	return
}

// Step flags for request strem processing.
const (
	RequestStepRequestLine = iota
	RequestStepHeader
	RequestStepBody
)

// Construct the request from the TCP stream.
//
// If TCP errors occur, err is not nil and req is nil.
// Request header must contain the Content-Length.
func (hc *httpConn) constructReq() (req *Request, err error) {
	// TODO
	reader := bufio.NewReaderSize(hc.rw, ServerRequestBufSize)
	req = &Request{}
	req.Header = make(map[string]string)
	step := RequestStepRequestLine
	finish := false
	for !finish {
		if line, err := reader.ReadString('\n'); err == nil {
			if line[len(line)-1] == '\n' {
				drop := 1
				if len(line) > 1 && line[len(line)-2] == '\r' {
					drop = 2
				}
				line = line[:len(line)-drop]
			}
			// line = line[:(len(line) - 1)]
			// process the line
			switch step {
			case RequestStepRequestLine:
				{
					reqLineWords := strings.SplitN(line, " ", 3)
					req.Method = reqLineWords[0]
					urlObj, err := url.ParseRequestURI(reqLineWords[1])
					if err != nil {
						return nil, err
					}
					req.URL = urlObj
					req.Proto = reqLineWords[2]
					step = RequestStepHeader
				}
			case RequestStepHeader:
				{
					if len(line) != 0 {
						// this line is header
						headerWords := strings.SplitN(line, ": ", 2)
						req.Header[headerWords[0]] = headerWords[1]
					} else {
						// this line is CRLF
						step = RequestStepBody
						switch req.Method {
						case MethodGet:
							{
								req.ContentLength = 0
							}
						case MethodPost:
							{
								cLenStr, ok := req.Header[HeaderContentLength]
								if !ok {
									return nil, errors.New("No Content-Length in POST request header")
								}
								cLen, err := strconv.Atoi(cLenStr)
								if err != nil {
									return nil, errors.New("Content-Length must be numeric")
								}
								req.ContentLength = int64(cLen)
							}
						}
						req.Body = &io.LimitedReader{
							R: reader,
							N: req.ContentLength,
						}
						finish = true
					}
				}
			}
		} else if err == io.EOF {
			return nil, err
		} else {
			return nil, err
		}

	}
	return req, nil
}
