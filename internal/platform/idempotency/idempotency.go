package idempotency

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"

	"dorm-hub/internal/domain"
	"github.com/jackc/pgx/v5"
)

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)

func Validate(key string) error {
	if !keyPattern.MatchString(key) {
		return domain.Invalid("Нужен корректный Idempotency-Key. Обновите страницу")
	}
	return nil
}

func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

type Querier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func Lookup(ctx context.Context, q Querier, user int64, scope, key, hash string) (string, bool, error) {
	if key == "" {
		return "", false, nil
	}
	var storedHash, result string
	err := q.QueryRow(ctx, `SELECT request_hash,result_id FROM idempotency_keys WHERE user_id=$1 AND scope=$2 AND key=$3`, user, scope, key).Scan(&storedHash, &result)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if storedHash != hash {
		return "", false, domain.Conflict("Этот запрос уже использован с другими данными")
	}
	return result, true, nil
}

// The caller locks the owner before lookup and records the result in the same
// transaction as the resource. Concurrent retries therefore cannot both insert.
func Record(ctx context.Context, tx pgx.Tx, user int64, scope, key, hash, result string) error {
	if key == "" {
		return nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO idempotency_keys(user_id,scope,key,request_hash,result_id) VALUES($1,$2,$3,$4,$5)`, user, scope, key, hash, result)
	return err
}
