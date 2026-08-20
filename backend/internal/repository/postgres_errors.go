package repository

import (
	"errors"

	"github.com/lib/pq"
)

func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr != nil && pqErr.Code == "23505"
}
