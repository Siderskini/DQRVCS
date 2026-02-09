package gossip

import (
	"encoding/json"
	"testing"
	"time"
)

func TestConsensusConfigRoundTrip(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	cfg, err := s.ConsensusConfig()
	if err != nil {
		t.Fatalf("read default config: %v", err)
	}
	if cfg.Threshold != 0.5 {
		t.Fatalf("unexpected default threshold: %v", cfg.Threshold)
	}

	cfg, err = s.SaveConsensusConfig(ConsensusConfig{
		Threshold: 0.67,
		Members:   []string{"node-b", "node-a", "node-b"},
	})
	if err != nil {
		t.Fatalf("save config: %v", err)
	}
	if cfg.Threshold != 0.67 {
		t.Fatalf("unexpected stored threshold: %v", cfg.Threshold)
	}
	if len(cfg.Members) != 2 || cfg.Members[0] != "node-a" || cfg.Members[1] != "node-b" {
		t.Fatalf("members not normalized: %v", cfg.Members)
	}

	cfg2, err := s.ConsensusConfig()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if cfg2.Threshold != cfg.Threshold {
		t.Fatalf("threshold mismatch after reload: %v != %v", cfg2.Threshold, cfg.Threshold)
	}
}

func TestProposalVoteCertFlow(t *testing.T) {
	storeA, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("open storeA: %v", err)
	}
	storeB, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("open storeB: %v", err)
	}

	_, err = storeA.SaveConsensusConfig(ConsensusConfig{
		Threshold: 0.5,
		Members:   []string{storeA.NodeID(), storeB.NodeID()},
	})
	if err != nil {
		t.Fatalf("save config: %v", err)
	}

	proposalOp, proposal, err := storeA.ProposeRefUpdate(ProposeRefInput{
		Ref:    "refs/heads/main",
		OldOID: "1111111",
		NewOID: "2222222",
		Epoch:  1,
		TTL:    time.Hour,
	})
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	if proposal.ProposalID == "" {
		t.Fatalf("expected generated proposal ID")
	}

	added, err := storeB.AddRemoteOp(proposalOp)
	if err != nil || !added {
		t.Fatalf("storeB add proposal op: added=%v err=%v", added, err)
	}

	voteBOp, voteBPayload, err := storeB.CastVote(proposal.ProposalID, "yes")
	if err != nil {
		t.Fatalf("storeB cast vote: %v", err)
	}
	if voteBPayload.Decision != "yes" {
		t.Fatalf("unexpected vote decision: %s", voteBPayload.Decision)
	}

	if added, err := storeA.AddRemoteOp(voteBOp); err != nil || !added {
		t.Fatalf("storeA add voteB op: added=%v err=%v", added, err)
	}

	status, err := storeA.ProposalStatus(proposal.ProposalID)
	if err != nil {
		t.Fatalf("status after one vote: %v", err)
	}
	if status.HasQuorum {
		t.Fatalf("expected no quorum with 1 yes out of 2 members")
	}

	if _, _, err := storeA.CastVote(proposal.ProposalID, "yes"); err != nil {
		t.Fatalf("storeA cast yes: %v", err)
	}
	status, err = storeA.ProposalStatus(proposal.ProposalID)
	if err != nil {
		t.Fatalf("status after second vote: %v", err)
	}
	if !status.HasQuorum {
		t.Fatalf("expected quorum with 2 yes out of 2 members")
	}

	certOp, certPayload, err := storeA.CertifyProposal(proposal.ProposalID, false)
	if err != nil {
		t.Fatalf("certify: %v", err)
	}
	if !certPayload.Certified {
		t.Fatalf("expected certified payload")
	}
	if certOp.Type != OpTypeConsensusCert {
		t.Fatalf("unexpected cert op type: %s", certOp.Type)
	}

	finalStatus, err := storeA.ProposalStatus(proposal.ProposalID)
	if err != nil {
		t.Fatalf("final status: %v", err)
	}
	if !finalStatus.Certified || finalStatus.CertifiedOpID == "" {
		t.Fatalf("expected proposal to be certified in final status")
	}
}

func TestProposalSummariesAndStatus(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	for i := 0; i < 3; i++ {
		_, _, err := s.ProposeRefUpdate(ProposeRefInput{
			Ref:    "refs/heads/main",
			OldOID: "old",
			NewOID: "new",
			Epoch:  uint64(i),
			TTL:    time.Hour,
		})
		if err != nil {
			t.Fatalf("append proposal %d: %v", i, err)
		}
	}

	summaries := s.ProposalSummaries(2)
	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries with limit=2, got %d", len(summaries))
	}
	for _, summary := range summaries {
		status, err := s.ProposalStatus(summary.ProposalID)
		if err != nil {
			t.Fatalf("status for %s: %v", summary.ProposalID, err)
		}
		if status.Proposal.ProposalID != summary.ProposalID {
			t.Fatalf("status/proposal mismatch: %s != %s", status.Proposal.ProposalID, summary.ProposalID)
		}
	}
}

func TestCastVoteRejectsExpiredProposal(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	_, proposal, err := s.ProposeRefUpdate(ProposeRefInput{
		Ref:    "refs/heads/main",
		OldOID: "a",
		NewOID: "b",
		Epoch:  0,
		TTL:    time.Nanosecond,
	})
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	time.Sleep(2 * time.Millisecond)

	if _, _, err := s.CastVote(proposal.ProposalID, "yes"); err == nil {
		t.Fatalf("expected vote on expired proposal to fail")
	}
}

func TestCertifyForceWithoutQuorum(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	_, err = s.SaveConsensusConfig(ConsensusConfig{
		Threshold: 0.9,
		Members:   []string{s.NodeID(), "peer-node"},
	})
	if err != nil {
		t.Fatalf("save config: %v", err)
	}

	_, proposal, err := s.ProposeRefUpdate(ProposeRefInput{
		Ref:    "refs/heads/main",
		OldOID: "a",
		NewOID: "b",
		Epoch:  2,
		TTL:    time.Hour,
	})
	if err != nil {
		t.Fatalf("propose: %v", err)
	}

	if _, _, err := s.CastVote(proposal.ProposalID, "yes"); err != nil {
		t.Fatalf("vote: %v", err)
	}

	if _, _, err := s.CertifyProposal(proposal.ProposalID, false); err == nil {
		t.Fatalf("expected non-force certify to fail without quorum")
	}

	op, cert, err := s.CertifyProposal(proposal.ProposalID, true)
	if err != nil {
		t.Fatalf("force certify: %v", err)
	}
	if op.Type != OpTypeConsensusCert {
		t.Fatalf("unexpected op type: %s", op.Type)
	}
	if cert.Certified {
		t.Fatalf("expected forced cert payload to indicate uncertified quorum state")
	}
}

func TestProposalPayloadIsValidJSON(t *testing.T) {
	s, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	op, _, err := s.ProposeRefUpdate(ProposeRefInput{
		Ref:    "refs/heads/main",
		OldOID: "x",
		NewOID: "y",
		Epoch:  0,
		TTL:    time.Minute,
	})
	if err != nil {
		t.Fatalf("propose: %v", err)
	}

	var payload ProposalPayload
	if err := json.Unmarshal(op.Payload, &payload); err != nil {
		t.Fatalf("proposal payload not valid JSON: %v", err)
	}
}
