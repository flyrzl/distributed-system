package http

//
// Http client library.
// Support concurrent and keep-alive (persistent) http requests.
// Not support: chuck transfer encoding.

import (
	"bufio"
	"d-s-zl/util"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
)

// Client send the http request and recevice response.
//
// Supports concurrency on multiple TCP connections.
type Client struct {
	// connSize    int
	// maxConnSize int

	// Your data here.
	connPools *util.ResourcePoolsMap
	mu        sync.Mutex
	cond      sync.Cond
}

// DefaultMaxConnSize is the default max size of
// active tcp connections.
const DefaultMaxConnSize = 500

// Step flags for response stream processing.
const (
	ResponseStepStatusLine = iota
	ResponseStepHeader
	ResponseStepBody
)

// NewClient initilize a Client with DefaultMaxConnSize.
func NewClient() *Client {
	return NewClientSize(DefaultMaxConnSize)
}

// NewClientSize initilize a Client with a specific maxConnSize.
func NewClientSize(maxConnSize int) *Client {
	// c := &Client{maxConnSize: maxConnSize}

	// Your initialization code here.
	c := &Client{}
	c.connPools = util.NewResourcePoolsMap(
		// create a resource
		func(host string) func() util.Resource {
			// nothing.
			return func() util.Resource {
				// resource is a tcp conn
				conn, err := net.Dial("tcp", host)
				if err != nil {
					return nil
				}
				return conn
			}
		},
		maxConnSize,
	)
	c.cond = sync.Cond{L: &c.mu}

	return c
}

// Get implements GET Method of HTTP/1.1.
//
// Must set the body and following headers in the request:
// * Content-Length
// * Host
func (c *Client) Get(URL string) (resp *Response, err error) {
	urlObj, err := url.ParseRequestURI(URL)
	if err != nil {
		return
	}
	header := make(map[string]string)
	header[HeaderContentLength] = "0"
	header[HeaderHost] = urlObj.Host
	req := &Request{
		Method:        MethodGet,
		URL:           urlObj,
		Proto:         HTTPVersion,
		Header:        header,
		ContentLength: 0,
		Body:          strings.NewReader(""),
	}
	resp, err = c.Send(req)
	return
}

// Post implements POST Method of HTTP/1.1.
//
// Must set the body and following headers in the request:
// * Content-Length
// * Host
//
// Write the contentLength bytes data into the body of HTTP request.
// Discard the sequential data after the reading contentLength bytes
// from body(io.Reader).
func (c *Client) Post(URL string, contentLength int64, body io.Reader) (resp *Response, err error) {
	urlObj, err := url.ParseRequestURI(URL)
	if err != nil {
		return
	}
	header := make(map[string]string)
	header[HeaderContentLength] = strconv.FormatInt(contentLength, 10)
	header[HeaderHost] = urlObj.Host
	req := &Request{
		Method:        MethodPost,
		URL:           urlObj,
		Proto:         HTTPVersion,
		Header:        header,
		ContentLength: contentLength,
		Body:          body,
	}
	resp, err = c.Send(req)
	return
}

// Send http request and returns an HTTP response.
//
// An error is returned if caused by client policy (such as invalid
// HTTP response), or failure to speak HTTP (such as a network
// connectivity problem).
//
// Note that a non-2xx status code doesn't mean any above errors.
//
// If the returned error is nil, the Response will contain a non-nil
// Body which is the caller's responsibility to close. If the Body is
// not closed, the Client may not be able to reuse a keep-alive TCP
// connection to the same server.
func (c *Client) Send(req *Request) (resp *Response, err error) {
	if req.URL == nil {
		return nil, errors.New("http: nil Request.URL")
	}

	// Get a available connection to the host for HTTP communication.
	conn, err := c.getConn(req.URL.Host)
	if err != nil {
		return nil, err
	}

	// Write the request to the TCP stream.
	err = c.writeReq(conn, req)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		c.cleanConn(conn, req)
		return nil, err
	}

	// Construct the response from the TCP stream.
	resp, err = c.constructResp(conn, req)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		c.cleanConn(conn, req)
	}
	return
}

// Put back the available connection of the specific host
// for the future use.
func (c *Client) putConn(conn io.ReadWriteCloser, host string) {
	// TODO

}

// Get a TCP connection to the host.
func (c *Client) getConn(host string) (conn io.ReadWriteCloser, err error) {
	// TODO
	return c.connPools.Get(host).(io.ReadWriteCloser), nil

}

// Clean one connection in the case of errors.
func (c *Client) cleanConn(conn io.ReadWriteCloser, req *Request) {
	// TODO
	c.connPools.Clean(req.URL.Host, conn)
}

// Write the request to TCP stream.
//
// The number of bytes in transmit body of a request must be more
// than the value of Content-Length header. If not, throws an error.
func (c *Client) writeReq(conn io.Writer, req *Request) (err error) {
	// TODO
	writer := bufio.NewWriterSize(conn, ClientRequestBufSize)
	// Write Request-Line
	reqLine := req.Method + " " + req.URL.Path + " " + req.Proto + "\n"
	_, err = writer.WriteString(reqLine)
	if err != nil {
		return err
	}
	// Write Headers
	for k, v := range req.Header {
		h := k + ": " + v + "\n"
		_, err = writer.WriteString(h)
		if err != nil {
			return err
		}
	}
	// Write CRLF
	err = writer.WriteByte('\n')
	if err != nil {
		return err
	}
	// Write message-body
	switch req.Method {
	case MethodGet:
		// nothing
	case MethodPost:
		{
			// copy the content of req.Body to writer's buf
			_, err = io.CopyN(writer, req.Body, req.ContentLength)
			if err != nil {
				return err
			}
		}
	}
	// write the data in the buf
	err = writer.Flush()
	return
}

// Construct response from the TCP stream.
//
// Body of the response will return io.EOF if reading
// Content-Length bytes.
//
// If TCP errors occur, err is not nil and req is nil.
func (c *Client) constructResp(conn io.Reader, req *Request) (resp *Response, err error) {
	// TODO
	reader := bufio.NewReaderSize(conn, ClientResponseBufSize)
	resp = &Response{}
	resp.Header = make(map[string]string)
	step := ResponseStepStatusLine
	finish := false
	for !finish {
		if line, err := reader.ReadString('\n'); err == nil {
			// line contains '\n'
			// line = line[:(len(line) - 1)]
			if line[len(line)-1] == '\n' {
				drop := 1
				if len(line) > 1 && line[len(line)-2] == '\r' {
					drop = 2
				}
				line = line[:len(line)-drop]
			}
			// process the line
			switch step {
			case ResponseStepStatusLine:
				{
					// process Status-Line
					statusLineWords := strings.SplitN(line, " ", 3)
					resp.Proto = statusLineWords[0]
					resp.StatusCode, err = strconv.Atoi(statusLineWords[1])
					resp.Status = statusLineWords[2]

					step = ResponseStepHeader
				}
			case ResponseStepHeader:
				{
					if len(line) != 0 {
						// process Headers
						headerWords := strings.SplitN(line, ": ", 2)
						resp.Header[headerWords[0]] = headerWords[1]
					} else {
						// process CRLF
						// the rest is body, process body
						step = ResponseStepBody
						contentLength, ok := resp.Header[HeaderContentLength]
						if !ok {
							return nil, errors.New("No Content-Length in Response header")
						}
						resp.ContentLength, _ = strconv.ParseInt(contentLength, 10, 64)
						resp.Body = &ResponseReader{
							c:    c,
							conn: conn,
							host: req.URL.Host,
							r: &io.LimitedReader{
								R: reader,
								N: resp.ContentLength,
							},
						}
						finish = true
						break
					}
				}
			}
		} else if err == io.EOF {
			break
		} else {
			return nil, err
		}
	}
	return resp, nil
}
