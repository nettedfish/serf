package agent

import (
	"bytes"
	"github.com/hashicorp/serf/client"
	"github.com/hashicorp/serf/serf"
	"github.com/hashicorp/serf/testutil"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// testRPCClient returns an RPCClient connected to an RPC server that
// serves only this connection.
func testRPCClient(t *testing.T) (*client.RPCClient, *Agent, *AgentIPC) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("err: %s", err)
	}

	lw := NewLogWriter(512)
	mult := io.MultiWriter(os.Stderr, lw)

	agent := testAgent(mult)
	ipc := NewAgentIPC(agent, "", l, mult, lw)

	rpcClient, err := client.NewRPCClient(l.Addr().String())
	if err != nil {
		t.Fatalf("err: %s", err)
	}

	return rpcClient, agent, ipc
}

func TestRPCClientForceLeave(t *testing.T) {
	client, a1, ipc := testRPCClient(t)
	a2 := testAgent(nil)
	defer ipc.Shutdown()
	defer client.Close()
	defer a1.Shutdown()
	defer a2.Shutdown()

	if err := a1.Start(); err != nil {
		t.Fatalf("err: %s", err)
	}

	if err := a2.Start(); err != nil {
		t.Fatalf("err: %s", err)
	}

	testutil.Yield()

	s2Addr := a2.conf.MemberlistConfig.BindAddr
	if _, err := a1.Join([]string{s2Addr}, false); err != nil {
		t.Fatalf("err: %s", err)
	}

	testutil.Yield()

	if err := a2.Shutdown(); err != nil {
		t.Fatalf("err: %s", err)
	}

	start := time.Now()
WAIT:
	time.Sleep(a1.conf.MemberlistConfig.ProbeInterval * 3)
	m := a1.Serf().Members()
	if len(m) != 2 {
		t.Fatalf("should have 2 members: %#v", a1.Serf().Members())
	}
	if m[1].Status != serf.StatusFailed && time.Now().Sub(start) < 3*time.Second {
		goto WAIT
	}

	if err := client.ForceLeave(a2.conf.NodeName); err != nil {
		t.Fatalf("err: %s", err)
	}

	m = a1.Serf().Members()
	if len(m) != 2 {
		t.Fatalf("should have 2 members: %#v", a1.Serf().Members())
	}

	if m[1].Status != serf.StatusLeft {
		t.Fatalf("should be left: %#v", m[1])
	}
}

func TestRPCClientJoin(t *testing.T) {
	client, a1, ipc := testRPCClient(t)
	a2 := testAgent(nil)
	defer ipc.Shutdown()
	defer client.Close()
	defer a1.Shutdown()
	defer a2.Shutdown()

	if err := a1.Start(); err != nil {
		t.Fatalf("err: %s", err)
	}

	if err := a2.Start(); err != nil {
		t.Fatalf("err: %s", err)
	}

	testutil.Yield()

	n, err := client.Join([]string{a2.conf.MemberlistConfig.BindAddr}, false)
	if err != nil {
		t.Fatalf("err: %s", err)
	}

	if n != 1 {
		t.Fatalf("n != 1: %d", n)
	}
}

func TestRPCClientMembers(t *testing.T) {
	client, a1, ipc := testRPCClient(t)
	a2 := testAgent(nil)
	defer ipc.Shutdown()
	defer client.Close()
	defer a1.Shutdown()
	defer a2.Shutdown()

	if err := a1.Start(); err != nil {
		t.Fatalf("err: %s", err)
	}

	if err := a2.Start(); err != nil {
		t.Fatalf("err: %s", err)
	}

	testutil.Yield()

	mem, err := client.Members()
	if err != nil {
		t.Fatalf("err: %s", err)
	}

	if len(mem) != 1 {
		t.Fatalf("bad: %#v", mem)
	}

	_, err = client.Join([]string{a2.conf.MemberlistConfig.BindAddr}, false)
	if err != nil {
		t.Fatalf("err: %s", err)
	}

	testutil.Yield()

	mem, err = client.Members()
	if err != nil {
		t.Fatalf("err: %s", err)
	}

	if len(mem) != 2 {
		t.Fatalf("bad: %#v", mem)
	}
}

func TestRPCClientMembersFiltered(t *testing.T) {
	client, a1, ipc := testRPCClient(t)
	a2 := testAgent(nil)
	defer ipc.Shutdown()
	defer client.Close()
	defer a1.Shutdown()
	defer a2.Shutdown()

	if err := a1.Start(); err != nil {
		t.Fatalf("err: %s", err)
	}

	if err := a2.Start(); err != nil {
		t.Fatalf("err: %s", err)
	}

	testutil.Yield()

	_, err := client.Join([]string{a2.conf.MemberlistConfig.BindAddr}, false)
	if err != nil {
		t.Fatalf("err: %s", err)
	}

	err = client.UpdateTags(map[string]string{
		"tag1": "val1",
		"tag2": "val2",
	}, []string{})

	if err != nil {
		t.Fatalf("bad: %s", err)
	}

	testutil.Yield()

	// Make sure that filters work on member names
	mem, err := client.MembersFiltered(map[string]string{}, "", "127.0.0.*")
	if err != nil {
		t.Fatalf("bad: %s", err)
	}

	if len(mem) == 0 {
		t.Fatalf("should have matched more than 0 members")
	}

	mem, err = client.MembersFiltered(map[string]string{}, "", "bad")
	if err != nil {
		t.Fatalf("bad: %s", err)
	}

	if len(mem) != 0 {
		t.Fatalf("should have matched 0 members: %#v", mem)
	}

	// Make sure that filters work on member tags
	mem, err = client.MembersFiltered(map[string]string{"tag1": "val.*"}, "", "")
	if err != nil {
		t.Fatalf("bad: %s", err)
	}

	if len(mem) != 1 {
		t.Fatalf("should have matched 1 member: %#v", mem)
	}

	// Make sure tag filters work on multiple tags
	mem, err = client.MembersFiltered(map[string]string{
		"tag1": "val.*",
		"tag2": "val2",
	}, "", "")

	if err != nil {
		t.Fatalf("bad: %s", err)
	}

	if len(mem) != 1 {
		t.Fatalf("should have matched one member: %#v", mem)
	}

	// Make sure all tags match when multiple tags are passed
	mem, err = client.MembersFiltered(map[string]string{
		"tag1": "val1",
		"tag2": "bad",
	}, "", "")

	if err != nil {
		t.Fatalf("bad: %s", err)
	}

	if len(mem) != 0 {
		t.Fatalf("should have matched 0 members: %#v", mem)
	}

	// Make sure that filters work on member status
	if err := client.ForceLeave(a2.conf.NodeName); err != nil {
		t.Fatalf("bad: %s", err)
	}

	mem, err = client.MembersFiltered(map[string]string{}, "alive", "")
	if err != nil {
		t.Fatalf("err: %s", err)
	}

	if len(mem) != 1 {
		t.Fatalf("should have matched 1 member: %#v", mem)
	}

	mem, err = client.MembersFiltered(map[string]string{}, "leaving", "")
	if err != nil {
		t.Fatalf("err: %s", err)
	}

	if len(mem) != 1 {
		t.Fatalf("should have matched 1 member: %#v", mem)
	}
}

func TestRPCClientUserEvent(t *testing.T) {
	client, a1, ipc := testRPCClient(t)
	defer ipc.Shutdown()
	defer client.Close()
	defer a1.Shutdown()

	handler := new(MockEventHandler)
	a1.RegisterEventHandler(handler)

	if err := a1.Start(); err != nil {
		t.Fatalf("err: %s", err)
	}

	testutil.Yield()

	if err := client.UserEvent("deploy", []byte("foo"), false); err != nil {
		t.Fatalf("err: %s", err)
	}

	testutil.Yield()

	handler.Lock()
	defer handler.Unlock()

	if len(handler.Events) == 0 {
		t.Fatal("no events")
	}

	serfEvent, ok := handler.Events[len(handler.Events)-1].(serf.UserEvent)
	if !ok {
		t.Fatalf("bad: %#v", serfEvent)
	}

	if serfEvent.Name != "deploy" {
		t.Fatalf("bad: %#v", serfEvent)
	}

	if string(serfEvent.Payload) != "foo" {
		t.Fatalf("bad: %#v", serfEvent)
	}
}

func TestRPCClientLeave(t *testing.T) {
	client, a1, ipc := testRPCClient(t)
	defer ipc.Shutdown()
	defer client.Close()
	defer a1.Shutdown()

	testutil.Yield()

	if err := client.Leave(); err != nil {
		t.Fatalf("err: %s", err)
	}

	testutil.Yield()

	select {
	case <-a1.ShutdownCh():
	default:
		t.Fatalf("agent should be shutdown!")
	}
}

func TestRPCClientMonitor(t *testing.T) {
	client, a1, ipc := testRPCClient(t)
	defer ipc.Shutdown()
	defer client.Close()
	defer a1.Shutdown()

	if err := a1.Start(); err != nil {
		t.Fatalf("err: %s", err)
	}

	eventCh := make(chan string, 64)
	if handle, err := client.Monitor("debug", eventCh); err != nil {
		t.Fatalf("err: %s", err)
	} else {
		defer client.Stop(handle)
	}

	testutil.Yield()

	select {
	case e := <-eventCh:
		if !strings.Contains(e, "Accepted client") {
			t.Fatalf("bad: %s", e)
		}
	default:
		t.Fatalf("should have backlog")
	}

	// Drain the rest of the messages as we know it
	drainEventCh(eventCh)

	// Join a bad thing to generate more events
	a1.Join(nil, false)

	testutil.Yield()

	select {
	case e := <-eventCh:
		if !strings.Contains(e, "joining") {
			t.Fatalf("bad: %s", e)
		}
	default:
		t.Fatalf("should have message")
	}
}

func TestRPCClientStream_User(t *testing.T) {
	client, a1, ipc := testRPCClient(t)
	defer ipc.Shutdown()
	defer client.Close()
	defer a1.Shutdown()

	if err := a1.Start(); err != nil {
		t.Fatalf("err: %s", err)
	}

	eventCh := make(chan map[string]interface{}, 64)
	if handle, err := client.Stream("user", eventCh); err != nil {
		t.Fatalf("err: %s", err)
	} else {
		defer client.Stop(handle)
	}

	testutil.Yield()

	if err := client.UserEvent("deploy", []byte("foo"), false); err != nil {
		t.Fatalf("err: %s", err)
	}

	testutil.Yield()

	select {
	case e := <-eventCh:
		if e["Event"].(string) != "user" {
			t.Fatalf("bad event: %#v", e)
		}
		if e["LTime"].(int64) != 1 {
			t.Fatalf("bad event: %#v", e)
		}
		if e["Name"].(string) != "deploy" {
			t.Fatalf("bad event: %#v", e)
		}
		if bytes.Compare(e["Payload"].([]byte), []byte("foo")) != 0 {
			t.Fatalf("bad event: %#v", e)
		}
		if e["Coalesce"].(bool) != false {
			t.Fatalf("bad event: %#v", e)
		}

	default:
		t.Fatalf("should have event")
	}
}

func TestRPCClientStream_Member(t *testing.T) {
	client, a1, ipc := testRPCClient(t)
	defer ipc.Shutdown()
	defer client.Close()
	defer a1.Shutdown()
	a2 := testAgent(nil)
	defer a2.Shutdown()

	if err := a1.Start(); err != nil {
		t.Fatalf("err: %s", err)
	}

	if err := a2.Start(); err != nil {
		t.Fatalf("err: %s", err)
	}

	testutil.Yield()

	eventCh := make(chan map[string]interface{}, 64)
	if handle, err := client.Stream("*", eventCh); err != nil {
		t.Fatalf("err: %s", err)
	} else {
		defer client.Stop(handle)
	}

	testutil.Yield()

	s2Addr := a2.conf.MemberlistConfig.BindAddr
	if _, err := a1.Join([]string{s2Addr}, false); err != nil {
		t.Fatalf("err: %s", err)
	}

	testutil.Yield()

	select {
	case e := <-eventCh:
		if e["Event"].(string) != "member-join" {
			t.Fatalf("bad event: %#v", e)
		}

		members := e["Members"].([]interface{})
		if len(members) != 1 {
			t.Fatalf("should have 1 member")
		}
		member := members[0].(map[interface{}]interface{})

		if _, ok := member["Name"].(string); !ok {
			t.Fatalf("bad event: %#v", e)
		}
		if _, ok := member["Addr"].([]uint8); !ok {
			t.Fatalf("bad event: %#v", e)
		}
		if _, ok := member["Port"].(uint64); !ok {
			t.Fatalf("bad event: %#v", e)
		}
		if _, ok := member["Tags"].(map[interface{}]interface{}); !ok {
			t.Fatalf("bad event: %#v", e)
		}
		if stat, _ := member["Status"].(string); stat != "alive" {
			t.Fatalf("bad event: %#v", e)
		}
		if _, ok := member["ProtocolMin"].(int64); !ok {
			t.Fatalf("bad event: %#v", e)
		}
		if _, ok := member["ProtocolMax"].(int64); !ok {
			t.Fatalf("bad event: %#v", e)
		}
		if _, ok := member["ProtocolCur"].(int64); !ok {
			t.Fatalf("bad event: %#v", e)
		}
		if _, ok := member["DelegateMin"].(int64); !ok {
			t.Fatalf("bad event: %#v", e)
		}
		if _, ok := member["DelegateMax"].(int64); !ok {
			t.Fatalf("bad event: %#v", e)
		}
		if _, ok := member["DelegateCur"].(int64); !ok {
			t.Fatalf("bad event: %#v", e)
		}

	default:
		t.Fatalf("should have event")
	}
}

func TestRPCClientUpdateTags(t *testing.T) {
	client, a1, ipc := testRPCClient(t)
	defer ipc.Shutdown()
	defer client.Close()
	defer a1.Shutdown()

	if err := a1.Start(); err != nil {
		t.Fatalf("err: %s", err)
	}

	testutil.Yield()

	mem, err := client.Members()
	if err != nil {
		t.Fatalf("err: %s", err)
	}

	if len(mem) != 1 {
		t.Fatalf("bad: %#v", mem)
	}

	m0 := mem[0]
	if _, ok := m0.Tags["testing"]; ok {
		t.Fatalf("have testing tag")
	}

	if err := client.UpdateTags(map[string]string{"testing": "1"}, nil); err != nil {
		t.Fatalf("err: %s", err)
	}

	mem, err = client.Members()
	if err != nil {
		t.Fatalf("err: %s", err)
	}

	if len(mem) != 1 {
		t.Fatalf("bad: %#v", mem)
	}

	m0 = mem[0]
	if _, ok := m0.Tags["testing"]; !ok {
		t.Fatalf("missing testing tag")
	}
}

func TestRPCClientQuery(t *testing.T) {
	cl, a1, ipc := testRPCClient(t)
	defer ipc.Shutdown()
	defer cl.Close()
	defer a1.Shutdown()

	handler := new(MockQueryHandler)
	handler.Response = []byte("ok")
	a1.RegisterEventHandler(handler)

	if err := a1.Start(); err != nil {
		t.Fatalf("err: %s", err)
	}

	testutil.Yield()

	ackCh := make(chan string, 1)
	respCh := make(chan client.NodeResponse, 1)
	params := client.QueryParam{
		RequestAck: true,
		Timeout:    200 * time.Millisecond,
		Name:       "deploy",
		Payload:    []byte("foo"),
		AckCh:      ackCh,
		RespCh:     respCh,
	}
	if err := cl.Query(&params); err != nil {
		t.Fatalf("err: %s", err)
	}

	testutil.Yield()

	handler.Lock()
	defer handler.Unlock()

	if len(handler.Queries) == 0 {
		t.Fatal("no queries")
	}

	query := handler.Queries[0]
	if query.Name != "deploy" {
		t.Fatalf("bad: %#v", query)
	}

	if string(query.Payload) != "foo" {
		t.Fatalf("bad: %#v", query)
	}

	select {
	case a := <-ackCh:
		if a != a1.conf.NodeName {
			t.Fatalf("Bad ack from: %v", a)
		}
	default:
		t.Fatalf("missing ack")
	}

	select {
	case r := <-respCh:
		if r.From != a1.conf.NodeName {
			t.Fatalf("Bad resp from: %v", r)
		}
		if string(r.Payload) != "ok" {
			t.Fatalf("Bad resp from: %v", r)
		}
	default:
		t.Fatalf("missing response")
	}
}

func TestRPCClientStream_Query(t *testing.T) {
	cl, a1, ipc := testRPCClient(t)
	defer ipc.Shutdown()
	defer cl.Close()
	defer a1.Shutdown()

	if err := a1.Start(); err != nil {
		t.Fatalf("err: %s", err)
	}

	eventCh := make(chan map[string]interface{}, 64)
	if handle, err := cl.Stream("query", eventCh); err != nil {
		t.Fatalf("err: %s", err)
	} else {
		defer cl.Stop(handle)
	}

	testutil.Yield()

	params := client.QueryParam{
		Timeout: 200 * time.Millisecond,
		Name:    "deploy",
		Payload: []byte("foo"),
	}
	if err := cl.Query(&params); err != nil {
		t.Fatalf("err: %s", err)
	}

	testutil.Yield()

	select {
	case e := <-eventCh:
		if e["Event"].(string) != "query" {
			t.Fatalf("bad query: %#v", e)
		}
		if e["ID"].(int64) != 1 {
			t.Fatalf("bad query: %#v", e)
		}
		if e["LTime"].(int64) != 1 {
			t.Fatalf("bad query: %#v", e)
		}
		if e["Name"].(string) != "deploy" {
			t.Fatalf("bad query: %#v", e)
		}
		if bytes.Compare(e["Payload"].([]byte), []byte("foo")) != 0 {
			t.Fatalf("bad query: %#v", e)
		}

	default:
		t.Fatalf("should have query")
	}
}

func TestRPCClientStream_Query_Respond(t *testing.T) {
	cl, a1, ipc := testRPCClient(t)
	defer ipc.Shutdown()
	defer cl.Close()
	defer a1.Shutdown()

	if err := a1.Start(); err != nil {
		t.Fatalf("err: %s", err)
	}

	eventCh := make(chan map[string]interface{}, 64)
	if handle, err := cl.Stream("query", eventCh); err != nil {
		t.Fatalf("err: %s", err)
	} else {
		defer cl.Stop(handle)
	}

	testutil.Yield()

	ackCh := make(chan string, 1)
	respCh := make(chan client.NodeResponse, 1)
	params := client.QueryParam{
		RequestAck: true,
		Timeout:    500 * time.Millisecond,
		Name:       "ping",
		AckCh:      ackCh,
		RespCh:     respCh,
	}
	if err := cl.Query(&params); err != nil {
		t.Fatalf("err: %s", err)
	}

	testutil.Yield()

	select {
	case e := <-eventCh:
		if e["Event"].(string) != "query" {
			t.Fatalf("bad query: %#v", e)
		}
		if e["Name"].(string) != "ping" {
			t.Fatalf("bad query: %#v", e)
		}

		// Send a response
		id := e["ID"].(int64)
		if err := cl.Respond(uint64(id), []byte("pong")); err != nil {
			t.Fatalf("err: %v", err)
		}

	default:
		t.Fatalf("should have query")
	}

	testutil.Yield()

	// Should have ack
	select {
	case a := <-ackCh:
		if a != a1.conf.NodeName {
			t.Fatalf("Bad ack from: %v", a)
		}
	default:
		t.Fatalf("missing ack")
	}

	// Should have response
	select {
	case r := <-respCh:
		if r.From != a1.conf.NodeName {
			t.Fatalf("Bad resp from: %v", r)
		}
		if string(r.Payload) != "pong" {
			t.Fatalf("Bad resp from: %v", r)
		}
	default:
		t.Fatalf("missing response")
	}
}

func TestRPCClientAuth(t *testing.T) {
	cl, a1, ipc := testRPCClient(t)
	defer ipc.Shutdown()
	defer cl.Close()
	defer a1.Shutdown()

	// Setup an auth key
	ipc.authKey = "foobar"

	if err := a1.Start(); err != nil {
		t.Fatalf("err: %s", err)
	}
	testutil.Yield()

	if err := cl.UserEvent("deploy", nil, false); err.Error() != authRequired {
		t.Fatalf("err: %s", err)
	}
	testutil.Yield()

	config := client.Config{Addr: ipc.listener.Addr().String(), AuthKey: "foobar"}
	rpcClient, err := client.ClientFromConfig(&config)
	if err != nil {
		t.Fatalf("err: %s", err)
	}
	defer rpcClient.Close()

	if err := rpcClient.UserEvent("deploy", nil, false); err != nil {
		t.Fatalf("err: %s", err)
	}
}
