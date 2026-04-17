package storage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"

	"go.etcd.io/bbolt"
)

// HashDescription returns the sha256 hex digest of a tool description.
// Used as the cache invalidation key inside EnrichedToolMeta.
func HashDescription(description string) string {
	sum := sha256.Sum256([]byte(description))
	return hex.EncodeToString(sum[:])
}

// SaveToolEnrichment persists an enrichment result. Overwrites any prior
// entry for the same (server, tool) pair.
func (b *BoltDB) SaveToolEnrichment(record *EnrichedToolMeta) error {
	return b.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(ToolEnrichmentBucket))
		data, err := record.MarshalBinary()
		if err != nil {
			return err
		}
		return bucket.Put([]byte(EnrichmentKey(record.ServerName, record.ToolName)), data)
	})
}

// GetToolEnrichment returns a cached enrichment if-and-only-if the stored
// DescriptionHash and PromptVersion both match the caller's expectations.
// A mismatch (or a missing entry) is reported as (nil, false, nil) — a
// cache miss that the caller should handle by triggering a fresh enrichment.
// Any actual I/O error is returned as-is.
func (b *BoltDB) GetToolEnrichment(serverName, toolName, descriptionHash string, promptVersion int) (*EnrichedToolMeta, bool, error) {
	var record *EnrichedToolMeta
	found := false

	err := b.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(ToolEnrichmentBucket))
		data := bucket.Get([]byte(EnrichmentKey(serverName, toolName)))
		if data == nil {
			return nil
		}

		candidate := &EnrichedToolMeta{}
		if err := candidate.UnmarshalBinary(data); err != nil {
			return err
		}

		if candidate.DescriptionHash != descriptionHash || candidate.PromptVersion != promptVersion {
			return nil
		}

		record = candidate
		found = true
		return nil
	})

	return record, found, err
}

// ListToolEnrichments returns every stored enrichment, optionally scoped
// to a single server.
func (b *BoltDB) ListToolEnrichments(serverName string) ([]*EnrichedToolMeta, error) {
	var records []*EnrichedToolMeta
	prefix := ""
	if serverName != "" {
		prefix = serverName + ":"
	}

	err := b.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(ToolEnrichmentBucket))
		return bucket.ForEach(func(k, v []byte) error {
			if prefix != "" && !bytes.HasPrefix(k, []byte(prefix)) {
				return nil
			}
			record := &EnrichedToolMeta{}
			if err := record.UnmarshalBinary(v); err != nil {
				return err
			}
			records = append(records, record)
			return nil
		})
	})

	return records, err
}

// DeleteToolEnrichment removes a single enrichment record.
func (b *BoltDB) DeleteToolEnrichment(serverName, toolName string) error {
	return b.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(ToolEnrichmentBucket))
		return bucket.Delete([]byte(EnrichmentKey(serverName, toolName)))
	})
}

// DeleteServerToolEnrichments removes every enrichment record for a server.
// Use when a server is removed from config.
func (b *BoltDB) DeleteServerToolEnrichments(serverName string) error {
	prefix := serverName + ":"
	return b.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(ToolEnrichmentBucket))
		var keysToDelete [][]byte
		err := bucket.ForEach(func(k, _ []byte) error {
			if bytes.HasPrefix(k, []byte(prefix)) {
				keysToDelete = append(keysToDelete, append([]byte(nil), k...))
			}
			return nil
		})
		if err != nil {
			return err
		}
		for _, k := range keysToDelete {
			if err := bucket.Delete(k); err != nil {
				return err
			}
		}
		return nil
	})
}
