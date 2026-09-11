package acp

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestNotificationAdmissionDoesNotConsumeCapacityOrWatermark(t *testing.T) {
	c, err := NewUnstartedConnection(nil, io.Discard, strings.NewReader(""), ConnectionOptions{MaxQueuedNotifications: 1, MaxNotificationBytes: 32, AcceptNotification: func(method string) bool { return method == "accepted" }})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for range 10000 {
		if !c.enqueueNotification(anyMessage{Method: "_unsupported", Params: json.RawMessage(`{"large":"ignored"}`)}) {
			t.Fatal("unsupported notification used capacity")
		}
	}
	if len(c.notificationQueue) != 0 || c.lastEnqueuedNotificationSeq != 0 || c.notificationBytes != 0 {
		t.Fatal("discarded notification changed ordered state")
	}
	if !c.enqueueNotification(anyMessage{Method: "accepted", Params: json.RawMessage(`{}`)}) {
		t.Fatal("supported notification rejected")
	}
	first := <-c.notificationQueue
	if first.seq != 1 {
		t.Fatalf("sequence=%d", first.seq)
	}
	c.processNotification(first)
	if c.notificationBytes != 0 || c.completedNotificationSeq != 1 {
		t.Fatal("notification completion did not release memory and advance watermark")
	}
}

func TestNotificationByteBudgetIncludesExecutingNotification(t *testing.T) {
	c, err := NewUnstartedConnection(nil, io.Discard, strings.NewReader(""), ConnectionOptions{MaxQueuedNotifications: 10, MaxNotificationBytes: 12})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	note := anyMessage{Method: "method", Params: json.RawMessage(`{}`)}
	if !c.enqueueNotification(note) {
		t.Fatal("first notification rejected")
	}
	first := <-c.notificationQueue
	if c.enqueueNotification(note) {
		t.Fatal("dequeue released in-flight payload budget too early")
	}
	c.processNotification(first)
	if !c.enqueueNotification(note) {
		t.Fatal("completed notification did not release byte budget")
	}
}

func TestNotificationFilterDoesNotInterceptRequestsOrCancellation(t *testing.T) {
	c, err := NewUnstartedConnection(nil, io.Discard, strings.NewReader(""), ConnectionOptions{AcceptNotification: func(string) bool { t.Error("control message passed to notification filter"); return false }})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	id := json.RawMessage(`1`)
	c.dispatchMessage(anyMessage{JSONRPC: "2.0", ID: &id, Method: "request"}, nil)
	c.dispatchMessage(anyMessage{JSONRPC: "2.0", Method: jsonRPCMethodCancelRequest, Params: json.RawMessage(`{"requestId":1}`)}, nil)
	request := <-c.requestQueue
	if request.ctx.Err() != context.Canceled {
		t.Fatal("cancellation did not bypass admission filter")
	}
}
