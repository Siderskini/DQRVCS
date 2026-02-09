package gossip

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	metadataDirName  = ".vcs"
	gossipDirName    = "gossip"
	identityFileName = "identity.json"
	peersFileName    = "peers.json"
	opsFileName      = "ops.log"
)

// Identity represents a node identity used to sign gossip operations.
type Identity struct {
	NodeID     string `json:"node_id"`
	PublicKey  string `json:"public_key"`
	PrivateKey string `json:"private_key"`
}

// Operation is a signed unit replicated across peers.
type Operation struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Author    string          `json:"author"`
	Seq       uint64          `json:"seq"`
	Timestamp string          `json:"timestamp"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	PublicKey string          `json:"public_key"`
	Signature string          `json:"signature"`
}

type peerFile struct {
	Peers []string `json:"peers"`
}

// Store provides thread-safe access to gossip metadata.
type Store struct {
	repoRoot string
	dir      string

	identity   Identity
	publicKey  ed25519.PublicKey
	privateKey ed25519.PrivateKey

	mu             sync.RWMutex
	ops            []Operation
	opIDs          map[string]struct{}
	seqByAuthor    map[string]uint64
	authorSeqIndex map[string]string
	peers          []string
}

// OpenStore opens or creates a repo-local gossip metadata store.
func OpenStore(repoRoot string) (*Store, error) {
	repoRoot = strings.TrimSpace(repoRoot)
	if repoRoot == "" {
		return nil, errors.New("repo root cannot be empty")
	}

	dir := filepath.Join(repoRoot, metadataDirName, gossipDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create gossip dir: %w", err)
	}

	s := &Store{
		repoRoot:       repoRoot,
		dir:            dir,
		opIDs:          map[string]struct{}{},
		seqByAuthor:    map[string]uint64{},
		authorSeqIndex: map[string]string{},
	}

	if err := s.loadIdentity(); err != nil {
		return nil, err
	}
	if err := s.loadPeers(); err != nil {
		return nil, err
	}
	if err := s.loadOps(); err != nil {
		return nil, err
	}

	return s, nil
}

// NodeID returns the local node ID.
func (s *Store) NodeID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.identity.NodeID
}

// IdentityPublic returns the local node identity without private key material.
func (s *Store) IdentityPublic() Identity {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return Identity{
		NodeID:    s.identity.NodeID,
		PublicKey: s.identity.PublicKey,
	}
}

// AppendLocalOp appends a new signed operation authored by this node.
func (s *Store) AppendLocalOp(opType string, payload json.RawMessage) (Operation, error) {
	opType = strings.TrimSpace(opType)
	if opType == "" {
		return Operation{}, errors.New("operation type is required")
	}
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	if !json.Valid(payload) {
		return Operation{}, errors.New("payload must be valid JSON")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	seq := s.seqByAuthor[s.identity.NodeID] + 1
	op := Operation{
		Type:      opType,
		Author:    s.identity.NodeID,
		Seq:       seq,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Payload:   cloneRawJSON(payload),
		PublicKey: s.identity.PublicKey,
	}

	signature, opID, err := signOperation(op, s.privateKey)
	if err != nil {
		return Operation{}, err
	}
	op.Signature = signature
	op.ID = opID

	added, err := s.addOperationLocked(op, true)
	if err != nil {
		return Operation{}, err
	}
	if !added {
		return Operation{}, errors.New("operation already exists")
	}
	return op, nil
}

// AddRemoteOp validates and appends an operation from a peer.
func (s *Store) AddRemoteOp(op Operation) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addOperationLocked(op, true)
}

// Summary returns max known sequence per author.
func (s *Store) Summary() map[string]uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]uint64, len(s.seqByAuthor))
	for author, seq := range s.seqByAuthor {
		out[author] = seq
	}
	return out
}

// MissingFor returns operations newer than the provided summary.
func (s *Store) MissingFor(summary map[string]uint64, limit int) []Operation {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if summary == nil {
		summary = map[string]uint64{}
	}

	ordered := make([]Operation, len(s.ops))
	copy(ordered, s.ops)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Author != ordered[j].Author {
			return ordered[i].Author < ordered[j].Author
		}
		return ordered[i].Seq < ordered[j].Seq
	})

	out := make([]Operation, 0, len(ordered))
	for _, op := range ordered {
		if op.Seq > summary[op.Author] {
			out = append(out, op)
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	return out
}

// Ops returns known operations in author/sequence order.
func (s *Store) Ops(limit int) []Operation {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ordered := make([]Operation, len(s.ops))
	copy(ordered, s.ops)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Author != ordered[j].Author {
			return ordered[i].Author < ordered[j].Author
		}
		return ordered[i].Seq < ordered[j].Seq
	})

	if limit > 0 && len(ordered) > limit {
		ordered = ordered[len(ordered)-limit:]
	}
	return ordered
}

// ListPeers returns configured peer base URLs.
func (s *Store) ListPeers() ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, len(s.peers))
	copy(out, s.peers)
	sort.Strings(out)
	return out, nil
}

// AddPeer adds a peer URL.
func (s *Store) AddPeer(raw string) (string, error) {
	normalized, err := normalizePeer(raw)
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, peer := range s.peers {
		if peer == normalized {
			return normalized, nil
		}
	}
	s.peers = append(s.peers, normalized)
	sort.Strings(s.peers)
	if err := s.savePeersLocked(); err != nil {
		return "", err
	}
	return normalized, nil
}

// RemovePeer removes a peer URL if present.
func (s *Store) RemovePeer(raw string) (string, error) {
	normalized, err := normalizePeer(raw)
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	filtered := s.peers[:0]
	removed := false
	for _, peer := range s.peers {
		if peer == normalized {
			removed = true
			continue
		}
		filtered = append(filtered, peer)
	}
	s.peers = filtered
	if !removed {
		return normalized, nil
	}
	if err := s.savePeersLocked(); err != nil {
		return "", err
	}
	return normalized, nil
}

func normalizePeer(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("peer address cannot be empty")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid peer URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errors.New("peer URL scheme must be http or https")
	}
	if u.Host == "" {
		return "", errors.New("peer URL host is required")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("peer URL cannot include query or fragment")
	}

	u.Path = strings.TrimSuffix(u.Path, "/")
	if u.Path == "/" {
		u.Path = ""
	}
	return u.String(), nil
}

func nodeIDFromPublicKey(pub []byte) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:16])
}

func canonicalOperationBytes(op Operation) ([]byte, error) {
	payload := cloneRawJSON(op.Payload)
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	if !json.Valid(payload) {
		return nil, errors.New("operation payload must be valid JSON")
	}

	type signable struct {
		Type      string          `json:"type"`
		Author    string          `json:"author"`
		Seq       uint64          `json:"seq"`
		Timestamp string          `json:"timestamp"`
		Payload   json.RawMessage `json:"payload"`
		PublicKey string          `json:"public_key"`
	}
	return json.Marshal(signable{
		Type:      op.Type,
		Author:    op.Author,
		Seq:       op.Seq,
		Timestamp: op.Timestamp,
		Payload:   payload,
		PublicKey: op.PublicKey,
	})
}

func computeOperationID(signable []byte, signature []byte) string {
	h := sha256.New()
	_, _ = h.Write(signable)
	_, _ = h.Write(signature)
	return hex.EncodeToString(h.Sum(nil))
}

func signOperation(op Operation, priv ed25519.PrivateKey) (string, string, error) {
	signable, err := canonicalOperationBytes(op)
	if err != nil {
		return "", "", err
	}
	sig := ed25519.Sign(priv, signable)
	sigB64 := base64.StdEncoding.EncodeToString(sig)
	return sigB64, computeOperationID(signable, sig), nil
}

func verifyOperation(op Operation) error {
	if strings.TrimSpace(op.ID) == "" {
		return errors.New("operation ID is required")
	}
	if strings.TrimSpace(op.Type) == "" {
		return errors.New("operation type is required")
	}
	if strings.TrimSpace(op.Author) == "" {
		return errors.New("operation author is required")
	}
	if op.Seq == 0 {
		return errors.New("operation sequence must be > 0")
	}
	if _, err := time.Parse(time.RFC3339Nano, op.Timestamp); err != nil {
		return fmt.Errorf("invalid timestamp: %w", err)
	}

	pubBytes, err := base64.StdEncoding.DecodeString(op.PublicKey)
	if err != nil {
		return fmt.Errorf("invalid public key encoding: %w", err)
	}
	if len(pubBytes) != ed25519.PublicKeySize {
		return errors.New("invalid public key length")
	}
	expectedAuthor := nodeIDFromPublicKey(pubBytes)
	if op.Author != expectedAuthor {
		return errors.New("operation author does not match public key")
	}

	signature, err := base64.StdEncoding.DecodeString(op.Signature)
	if err != nil {
		return fmt.Errorf("invalid signature encoding: %w", err)
	}
	if len(signature) != ed25519.SignatureSize {
		return errors.New("invalid signature length")
	}

	signable, err := canonicalOperationBytes(op)
	if err != nil {
		return err
	}
	if !ed25519.Verify(ed25519.PublicKey(pubBytes), signable, signature) {
		return errors.New("invalid signature")
	}

	expectedID := computeOperationID(signable, signature)
	if op.ID != expectedID {
		return errors.New("operation ID does not match content/signature")
	}
	return nil
}

func cloneRawJSON(in json.RawMessage) json.RawMessage {
	if len(in) == 0 {
		return nil
	}
	out := make([]byte, len(in))
	copy(out, in)
	return json.RawMessage(out)
}

func authorSeqKey(author string, seq uint64) string {
	return author + ":" + fmt.Sprintf("%d", seq)
}

func (s *Store) addOperationLocked(op Operation, persist bool) (bool, error) {
	if err := verifyOperation(op); err != nil {
		return false, err
	}
	if _, exists := s.opIDs[op.ID]; exists {
		return false, nil
	}

	key := authorSeqKey(op.Author, op.Seq)
	if existingID, exists := s.authorSeqIndex[key]; exists && existingID != op.ID {
		return false, fmt.Errorf("conflicting operations for %s seq=%d", op.Author, op.Seq)
	}

	op.Payload = cloneRawJSON(op.Payload)
	s.ops = append(s.ops, op)
	s.opIDs[op.ID] = struct{}{}
	s.authorSeqIndex[key] = op.ID
	if op.Seq > s.seqByAuthor[op.Author] {
		s.seqByAuthor[op.Author] = op.Seq
	}

	if persist {
		if err := s.appendOpLocked(op); err != nil {
			return false, err
		}
	}

	return true, nil
}

func (s *Store) appendOpLocked(op Operation) error {
	path := filepath.Join(s.dir, opsFileName)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open ops log: %w", err)
	}
	defer file.Close()

	encoded, err := json.Marshal(op)
	if err != nil {
		return fmt.Errorf("marshal operation: %w", err)
	}
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("append operation: %w", err)
	}
	return nil
}

func (s *Store) loadIdentity() error {
	path := filepath.Join(s.dir, identityFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s.generateIdentity(path)
		}
		return fmt.Errorf("read identity file: %w", err)
	}

	var identity Identity
	if err := json.Unmarshal(data, &identity); err != nil {
		return fmt.Errorf("decode identity file: %w", err)
	}
	pubBytes, err := base64.StdEncoding.DecodeString(identity.PublicKey)
	if err != nil {
		return fmt.Errorf("decode identity public key: %w", err)
	}
	privBytes, err := base64.StdEncoding.DecodeString(identity.PrivateKey)
	if err != nil {
		return fmt.Errorf("decode identity private key: %w", err)
	}
	if len(pubBytes) != ed25519.PublicKeySize || len(privBytes) != ed25519.PrivateKeySize {
		return errors.New("identity key lengths are invalid")
	}

	expectedID := nodeIDFromPublicKey(pubBytes)
	if identity.NodeID == "" {
		identity.NodeID = expectedID
	}
	if identity.NodeID != expectedID {
		return errors.New("identity node ID does not match public key")
	}
	if !strings.EqualFold(
		base64.StdEncoding.EncodeToString(ed25519.PrivateKey(privBytes).Public().(ed25519.PublicKey)),
		base64.StdEncoding.EncodeToString(pubBytes),
	) {
		return errors.New("identity private key does not match public key")
	}

	s.identity = identity
	s.publicKey = ed25519.PublicKey(pubBytes)
	s.privateKey = ed25519.PrivateKey(privBytes)
	return nil
}

func (s *Store) generateIdentity(path string) error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate identity keypair: %w", err)
	}
	identity := Identity{
		NodeID:     nodeIDFromPublicKey(pub),
		PublicKey:  base64.StdEncoding.EncodeToString(pub),
		PrivateKey: base64.StdEncoding.EncodeToString(priv),
	}
	if err := writeJSONAtomic(path, identity, 0o600); err != nil {
		return err
	}
	s.identity = identity
	s.publicKey = pub
	s.privateKey = priv
	return nil
}

func (s *Store) loadPeers() error {
	path := filepath.Join(s.dir, peersFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.peers = []string{}
			return nil
		}
		return fmt.Errorf("read peers file: %w", err)
	}

	var pf peerFile
	if err := json.Unmarshal(data, &pf); err != nil {
		return fmt.Errorf("decode peers file: %w", err)
	}

	normalized := make([]string, 0, len(pf.Peers))
	seen := map[string]struct{}{}
	for _, peer := range pf.Peers {
		norm, err := normalizePeer(peer)
		if err != nil {
			return err
		}
		if _, exists := seen[norm]; exists {
			continue
		}
		seen[norm] = struct{}{}
		normalized = append(normalized, norm)
	}
	sort.Strings(normalized)
	s.peers = normalized
	return nil
}

func (s *Store) savePeersLocked() error {
	path := filepath.Join(s.dir, peersFileName)
	return writeJSONAtomic(path, peerFile{Peers: s.peers}, 0o644)
}

func (s *Store) loadOps() error {
	path := filepath.Join(s.dir, opsFileName)
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("open ops log: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" {
			continue
		}

		var op Operation
		if err := json.Unmarshal([]byte(raw), &op); err != nil {
			return fmt.Errorf("decode operation at line %d: %w", line, err)
		}
		if _, err := s.addOperationLocked(op, false); err != nil {
			return fmt.Errorf("load operation at line %d: %w", line, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan ops log: %w", err)
	}
	return nil
}

func writeJSONAtomic(path string, v any, mode os.FileMode) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal JSON: %w", err)
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, mode); err != nil {
		return fmt.Errorf("write temp JSON file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename temp JSON file: %w", err)
	}
	return nil
}
