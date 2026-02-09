package gossip

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestOpenStoreIdentityPersists(t *testing.T) {
	root := t.TempDir()

	s1, err := OpenStore(root)
	if err != nil {
		t.Fatalf("open first store: %v", err)
	}
	id1 := s1.IdentityPublic()
	if id1.NodeID == "" || id1.PublicKey == "" {
		t.Fatalf("expected populated identity, got %+v", id1)
	}

	s2, err := OpenStore(root)
	if err != nil {
		t.Fatalf("open second store: %v", err)
	}
	id2 := s2.IdentityPublic()
	if id1.NodeID != id2.NodeID || id1.PublicKey != id2.PublicKey {
		t.Fatalf("identity did not persist: first=%+v second=%+v", id1, id2)
	}
}

func TestLocalOperationAppendAndTamperRejection(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	op, err := s.AppendLocalOp("git.commit", json.RawMessage(`{"hash":"abc"}`))
	if err != nil {
		t.Fatalf("append op: %v", err)
	}

	ops := s.Ops(0)
	if len(ops) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(ops))
	}
	if ops[0].ID != op.ID {
		t.Fatalf("stored operation mismatch: got=%s want=%s", ops[0].ID, op.ID)
	}

	tampered := op
	tampered.Payload = json.RawMessage(`{"hash":"tampered"}`)
	if _, err := s.AddRemoteOp(tampered); err == nil {
		t.Fatalf("expected tampered op to fail signature verification")
	}
}

func TestMissingForSummary(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	op1, err := s.AppendLocalOp("event.one", json.RawMessage(`{"v":1}`))
	if err != nil {
		t.Fatalf("append op1: %v", err)
	}
	if _, err := s.AppendLocalOp("event.two", json.RawMessage(`{"v":2}`)); err != nil {
		t.Fatalf("append op2: %v", err)
	}
	if _, err := s.AppendLocalOp("event.three", json.RawMessage(`{"v":3}`)); err != nil {
		t.Fatalf("append op3: %v", err)
	}

	missing := s.MissingFor(map[string]uint64{op1.Author: 1}, 0)
	if len(missing) != 2 {
		t.Fatalf("expected 2 missing ops, got %d", len(missing))
	}
	if missing[0].Seq != 2 || missing[1].Seq != 3 {
		t.Fatalf("unexpected missing seq values: %d, %d", missing[0].Seq, missing[1].Seq)
	}
}

func TestPeerNormalizationAndPersistence(t *testing.T) {
	root := t.TempDir()
	s, err := OpenStore(root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	peerA, err := s.AddPeer("127.0.0.1:8787/")
	if err != nil {
		t.Fatalf("add peerA: %v", err)
	}
	if _, err := s.AddPeer("http://127.0.0.1:8787"); err != nil {
		t.Fatalf("add duplicate peer: %v", err)
	}
	if peerA != "http://127.0.0.1:8787" {
		t.Fatalf("unexpected normalized peer: %s", peerA)
	}

	peers, err := s.ListPeers()
	if err != nil {
		t.Fatalf("list peers: %v", err)
	}
	if len(peers) != 1 {
		t.Fatalf("expected 1 deduped peer, got %d (%v)", len(peers), peers)
	}

	s2, err := OpenStore(root)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	peers2, err := s2.ListPeers()
	if err != nil {
		t.Fatalf("list peers after reopen: %v", err)
	}
	if len(peers2) != 1 || peers2[0] != "http://127.0.0.1:8787" {
		t.Fatalf("unexpected peers after reopen: %v", peers2)
	}

	if _, err := s2.RemovePeer("127.0.0.1:8787"); err != nil {
		t.Fatalf("remove peer: %v", err)
	}
	peers3, err := s2.ListPeers()
	if err != nil {
		t.Fatalf("list peers after remove: %v", err)
	}
	if len(peers3) != 0 {
		t.Fatalf("expected no peers after remove, got %v", peers3)
	}
}

func TestSyncPeerOfflineThenCatchup(t *testing.T) {
	storeA, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("open storeA: %v", err)
	}
	storeB, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("open storeB: %v", err)
	}

	if _, err := storeA.AppendLocalOp("commit", json.RawMessage(`{"id":"a1"}`)); err != nil {
		t.Fatalf("append a1: %v", err)
	}
	if _, err := storeA.AppendLocalOp("commit", json.RawMessage(`{"id":"a2"}`)); err != nil {
		t.Fatalf("append a2: %v", err)
	}

	if _, err := SyncPeer(context.Background(), storeA, "http://127.0.0.1:1", 128, 2, nil); err == nil {
		t.Fatalf("expected offline peer sync to fail")
	}

	serverB := httptest.NewServer(NewHandler(storeB, 128))
	defer serverB.Close()

	stats1, err := SyncPeer(context.Background(), storeA, serverB.URL, 128, 6, nil)
	if err != nil {
		t.Fatalf("sync storeA -> storeB: %v", err)
	}
	if stats1.Sent == 0 {
		t.Fatalf("expected sync to send operations to storeB")
	}

	opsB := storeB.Ops(0)
	if len(opsB) < 2 {
		t.Fatalf("expected storeB to receive at least 2 ops, got %d", len(opsB))
	}

	if _, err := storeB.AppendLocalOp("vote", json.RawMessage(`{"id":"b1"}`)); err != nil {
		t.Fatalf("append b1: %v", err)
	}

	stats2, err := SyncPeer(context.Background(), storeA, serverB.URL, 128, 6, nil)
	if err != nil {
		t.Fatalf("sync storeB -> storeA: %v", err)
	}
	if stats2.Pulled == 0 {
		t.Fatalf("expected storeA to pull new ops from storeB")
	}

	if len(storeA.Ops(0)) < 3 {
		t.Fatalf("expected storeA to have at least 3 ops after catch-up")
	}
}
