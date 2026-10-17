package auth

import "context"

// PruneExpired bounds retention of abandoned OTPs and expired sessions. Batches
// keep maintenance short. The outer expiry condition protects a concurrently
// renewed code after waiting for its row lock.
func (s *Service) PruneExpired(ctx context.Context) error {
	for _, query := range []string{
		`DELETE FROM login_codes WHERE expires_at < now()-interval '1 hour' AND email IN (SELECT email FROM login_codes WHERE expires_at < now()-interval '1 hour' ORDER BY expires_at LIMIT 500)`,
		`DELETE FROM account_deletion_codes WHERE expires_at < now()-interval '1 hour' AND user_id IN (SELECT user_id FROM account_deletion_codes WHERE expires_at < now()-interval '1 hour' ORDER BY expires_at LIMIT 500)`,
		`DELETE FROM sessions WHERE expires_at < now() AND token_hash IN (SELECT token_hash FROM sessions WHERE expires_at < now() ORDER BY expires_at LIMIT 500)`,
	} {
		if _, err := s.db.Exec(ctx, query); err != nil {
			return err
		}
	}
	return nil
}
