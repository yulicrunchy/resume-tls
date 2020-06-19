package resumetls

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"reflect"

	intio "github.com/igarciaolaizola/resume-tls/internal/io"
	intnet "github.com/igarciaolaizola/resume-tls/internal/net"
	intref "github.com/igarciaolaizola/resume-tls/internal/reflect"
)

// State is buffered handshake data
type State struct {
	conn   []byte
	rand   []byte
	inSeq  [8]byte
	outSeq [8]byte
}

// Conn resumable tls conn
type Conn struct {
	handshaked   bool
	overrideRand *intio.OverrideReader
	overrideConn *intnet.OverrideConn
	connBuffer   *bytes.Buffer
	randBuffer   *bytes.Buffer
	*tls.Conn
}

func (c *Conn) GetConn() *tls.Conn {
	return c.Conn
}

// Server returns a resumable tls conn
func Server(conn net.Conn, cfg *tls.Config, state *State) (*Conn, error) {
	if state != nil {
		return clientResume(conn, cfg, state)
	}
	return clientInitialize(conn, cfg), nil
}

// Client returs a resumable tls conn
func Client(conn net.Conn, cfg *tls.Config, state *State) (*Conn, error) {
	if state != nil {
		return clientResume(conn, cfg, state)
	}
	return clientInitialize(conn, cfg), nil
}

// client initializes a resumable TLS client conn
func clientInitialize(conn net.Conn, cfg *tls.Config) *Conn {
	connBuf := &bytes.Buffer{}
	randBuf := &bytes.Buffer{}

	rnd := cfg.Rand
	if rnd == nil {
		rnd = rand.Reader
	}
	ovRand := &intio.OverrideReader{
		OverrideReader: io.TeeReader(rnd, randBuf),
		Reader:         rnd,
	}
	ovConn := &intnet.OverrideConn{
		Conn:           conn,
		OverrideReader: io.TeeReader(conn, connBuf),
	}

	cfg.Rand = ovRand
	return &Conn{
		overrideConn: ovConn,
		overrideRand: ovRand,
		connBuffer:   connBuf,
		randBuffer:   randBuf,
		Conn:         tls.Server(ovConn, cfg),
	}
}

// clientResume resumes a resumable TLS client conn
func clientResume(conn net.Conn, cfg *tls.Config, state *State) (*Conn, error) {
	rnd := cfg.Rand
	if rnd == nil {
		rnd = rand.Reader
	}
	ovRand := &intio.OverrideReader{
		OverrideReader: io.MultiReader(bytes.NewBuffer(state.rand), rnd),
		Reader:         rnd,
	}
	ovConn := &intnet.OverrideConn{
		Conn:           conn,
		OverrideReader: io.MultiReader(bytes.NewBuffer(state.conn), conn),
		OverrideWriter: ioutil.Discard,
	}
	cfg.Rand = ovRand

	cli := tls.Server(ovConn, cfg)
	if err := cli.Handshake(); err != nil {
		return nil, fmt.Errorf("handshake er: %v", err)
	}
	ovRand.OverrideReader = nil
	ovConn.OverrideReader = nil
	ovConn.OverrideWriter = nil
	setSeq(cli, state.inSeq, state.outSeq)

	return &Conn{
		handshaked: true,
		connBuffer: bytes.NewBuffer(state.conn),
		randBuffer: bytes.NewBuffer(state.rand),
		Conn:       cli,
	}, nil
}

// Handshake overrides tls handshakes
func (c *Conn) Handshake() error {
	if c.handshaked {
		return nil
	}
	if err := c.Conn.Handshake(); err != nil {
		c.connBuffer = &bytes.Buffer{}
		c.randBuffer = &bytes.Buffer{}
		return err
	}
	c.handshaked = true
	c.overrideRand.OverrideReader = nil
	c.overrideConn.OverrideReader = nil
	return nil
}

// State gets the data in order to resume a connection
func (c *Conn) State() *State {
	in, out := getSeq(c.Conn)
	return &State{
		conn:   c.connBuffer.Bytes(),
		rand:   c.randBuffer.Bytes(),
		inSeq:  in,
		outSeq: out,
	}
}

// setSeq override sequence number
func setSeq(conn *tls.Conn, in [8]byte, out [8]byte) {
	r := reflect.ValueOf(conn).Elem()
	fIn := r.FieldByName("in")
	fOut := r.FieldByName("out")

	intref.SetFieldValue(fIn, "seq", in)
	intref.SetFieldValue(fOut, "seq", out)
}

// getSeq obtains sequence numbers
func getSeq(conn *tls.Conn) ([8]byte, [8]byte) {
	r := reflect.ValueOf(conn).Elem()
	fIn := r.FieldByName("in")
	in := intref.FieldToInterface(fIn, "seq").([8]byte)

	fOut := r.FieldByName("out")
	out := intref.FieldToInterface(fOut, "seq").([8]byte)

	return in, out
}
