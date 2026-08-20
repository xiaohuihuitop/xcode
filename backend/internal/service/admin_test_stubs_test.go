//go:build unit

package service

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

type userRepoStub struct {
	user           *User
	getErr         error
	createErr      error
	deleteErr      error
	exists         bool
	existsErr      error
	aliasExists    bool
	aliasErr       error
	guardedCreates int
	nextID         int64
	created        []*User
	updated        []*User
	deletedIDs     []int64
	usersByEmail   map[string]*User
	getByEmailErr  error
}

func (s *userRepoStub) Create(ctx context.Context, user *User) error {
	if s.createErr != nil {
		return s.createErr
	}
	if s.nextID != 0 && user.ID == 0 {
		user.ID = s.nextID
	}
	s.created = append(s.created, user)
	if s.usersByEmail == nil {
		s.usersByEmail = make(map[string]*User)
	}
	s.usersByEmail[user.Email] = user
	s.user = user
	return nil
}

func (s *userRepoStub) CreateWithEmailAliasGuard(ctx context.Context, user *User) error {
	s.guardedCreates++
	if s.aliasErr != nil {
		return s.aliasErr
	}
	if s.aliasExists {
		return ErrEmailExists
	}
	return s.Create(ctx, user)
}

func (s *userRepoStub) GetByID(ctx context.Context, id int64) (*User, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	if s.user == nil {
		return nil, ErrUserNotFound
	}
	return s.user, nil
}

func (s *userRepoStub) GetByEmail(ctx context.Context, email string) (*User, error) {
	if s.getByEmailErr != nil {
		return nil, s.getByEmailErr
	}
	if s.usersByEmail != nil {
		if user, ok := s.usersByEmail[email]; ok {
			return user, nil
		}
	}
	if s.user != nil && s.user.Email == email {
		return s.user, nil
	}
	return nil, ErrUserNotFound
}

func (s *userRepoStub) GetFirstAdmin(ctx context.Context) (*User, error) {
	panic("unexpected GetFirstAdmin call")
}

func (s *userRepoStub) Update(ctx context.Context, user *User, fields UserUpdateFields) error {
	s.updated = append(s.updated, user)
	if s.usersByEmail == nil {
		s.usersByEmail = make(map[string]*User)
	}
	s.usersByEmail[user.Email] = user
	s.user = user
	return nil
}

func (s *userRepoStub) Delete(ctx context.Context, id int64) error {
	s.deletedIDs = append(s.deletedIDs, id)
	return s.deleteErr
}

func (s *userRepoStub) GetUserAvatar(ctx context.Context, userID int64) (*UserAvatar, error) {
	panic("unexpected GetUserAvatar call")
}

func (s *userRepoStub) UpsertUserAvatar(ctx context.Context, userID int64, input UpsertUserAvatarInput) (*UserAvatar, error) {
	panic("unexpected UpsertUserAvatar call")
}

func (s *userRepoStub) DeleteUserAvatar(ctx context.Context, userID int64) error {
	panic("unexpected DeleteUserAvatar call")
}

func (s *userRepoStub) List(ctx context.Context, params pagination.PaginationParams) ([]User, *pagination.PaginationResult, error) {
	panic("unexpected List call")
}

func (s *userRepoStub) ListWithFilters(ctx context.Context, params pagination.PaginationParams, filters UserListFilters) ([]User, *pagination.PaginationResult, error) {
	panic("unexpected ListWithFilters call")
}

func (s *userRepoStub) GetLatestUsedAtByUserIDs(ctx context.Context, userIDs []int64) (map[int64]*time.Time, error) {
	panic("unexpected GetLatestUsedAtByUserIDs call")
}

func (s *userRepoStub) GetLatestUsedAtByUserID(ctx context.Context, userID int64) (*time.Time, error) {
	panic("unexpected GetLatestUsedAtByUserID call")
}

func (s *userRepoStub) UpdateUserLastActiveAt(ctx context.Context, userID int64, activeAt time.Time) error {
	panic("unexpected UpdateUserLastActiveAt call")
}

func (s *userRepoStub) UpdateBalance(ctx context.Context, id int64, amount float64) error {
	panic("unexpected UpdateBalance call")
}

func (s *userRepoStub) DeductBalance(ctx context.Context, id int64, amount float64) error {
	panic("unexpected DeductBalance call")
}

func (s *userRepoStub) AdjustBalance(ctx context.Context, id int64, delta float64) (BalanceChange, error) {
	panic("unexpected AdjustBalance call")
}

func (s *userRepoStub) SetBalance(ctx context.Context, id int64, value float64) (BalanceChange, error) {
	panic("unexpected SetBalance call")
}

func (s *userRepoStub) UpdateConcurrency(ctx context.Context, id int64, amount int) error {
	panic("unexpected UpdateConcurrency call")
}

func (s *userRepoStub) BatchSetConcurrency(context.Context, []int64, int) (int, error) { return 0, nil }
func (s *userRepoStub) BatchAddConcurrency(context.Context, []int64, int) (int, error) { return 0, nil }
func (s *userRepoStub) BatchUpdateLimits(context.Context, []int64, *int, *int) (int, error) {
	return 0, nil
}

func (s *userRepoStub) ExistsByEmail(ctx context.Context, email string) (bool, error) {
	if s.existsErr != nil {
		return false, s.existsErr
	}
	return s.exists, nil
}

func (s *userRepoStub) ExistsByEmailAlias(ctx context.Context, email string) (bool, error) {
	if s.aliasErr != nil {
		return false, s.aliasErr
	}
	return s.aliasExists, nil
}

func (s *userRepoStub) ListUserAuthIdentities(ctx context.Context, userID int64) ([]UserAuthIdentityRecord, error) {
	panic("unexpected ListUserAuthIdentities call")
}

func (s *userRepoStub) UnbindUserAuthProvider(context.Context, int64, string) error {
	panic("unexpected UnbindUserAuthProvider call")
}

func (s *userRepoStub) UpdateTotpSecret(ctx context.Context, userID int64, encryptedSecret *string) error {
	panic("unexpected UpdateTotpSecret call")
}

func (s *userRepoStub) EnableTotp(ctx context.Context, userID int64) error {
	panic("unexpected EnableTotp call")
}

func (s *userRepoStub) DisableTotp(ctx context.Context, userID int64) error {
	panic("unexpected DisableTotp call")
}

func (s *userRepoStub) GetByIDIncludeDeleted(ctx context.Context, id int64) (*User, error) {
	return s.GetByID(ctx, id)
}

type proxyRepoStub struct {
	deleteErr    error
	countErr     error
	accountCount int64
	deletedIDs   []int64
}

func (s *proxyRepoStub) Create(ctx context.Context, proxy *Proxy) error {
	panic("unexpected Create call")
}

func (s *proxyRepoStub) GetByID(ctx context.Context, id int64) (*Proxy, error) {
	panic("unexpected GetByID call")
}

func (s *proxyRepoStub) ListByIDs(ctx context.Context, ids []int64) ([]Proxy, error) {
	panic("unexpected ListByIDs call")
}

func (s *proxyRepoStub) Update(ctx context.Context, proxy *Proxy) error {
	panic("unexpected Update call")
}

func (s *proxyRepoStub) Delete(ctx context.Context, id int64) error {
	s.deletedIDs = append(s.deletedIDs, id)
	return s.deleteErr
}

func (s *proxyRepoStub) List(ctx context.Context, params pagination.PaginationParams) ([]Proxy, *pagination.PaginationResult, error) {
	panic("unexpected List call")
}

func (s *proxyRepoStub) ListWithFilters(ctx context.Context, params pagination.PaginationParams, protocol, status, search string) ([]Proxy, *pagination.PaginationResult, error) {
	panic("unexpected ListWithFilters call")
}

func (s *proxyRepoStub) ListActive(ctx context.Context) ([]Proxy, error) {
	panic("unexpected ListActive call")
}

func (s *proxyRepoStub) ListActiveWithAccountCount(ctx context.Context) ([]ProxyWithAccountCount, error) {
	panic("unexpected ListActiveWithAccountCount call")
}

func (s *proxyRepoStub) ListWithFiltersAndAccountCount(ctx context.Context, params pagination.PaginationParams, protocol, status, search string) ([]ProxyWithAccountCount, *pagination.PaginationResult, error) {
	panic("unexpected ListWithFiltersAndAccountCount call")
}

func (s *proxyRepoStub) ExistsByHostPortAuth(ctx context.Context, host string, port int, username, password string) (bool, error) {
	panic("unexpected ExistsByHostPortAuth call")
}

func (s *proxyRepoStub) CountAccountsByProxyID(ctx context.Context, proxyID int64) (int64, error) {
	if s.countErr != nil {
		return 0, s.countErr
	}
	return s.accountCount, nil
}

func (s *proxyRepoStub) ListAccountSummariesByProxyID(ctx context.Context, proxyID int64) ([]ProxyAccountSummary, error) {
	panic("unexpected ListAccountSummariesByProxyID call")
}
func (s *proxyRepoStub) SweepExpiredProxies(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}
func (s *proxyRepoStub) ListAllForFallback(_ context.Context) ([]Proxy, error) {
	return nil, nil
}
func (s *proxyRepoStub) CountExpired(_ context.Context) (int64, error) {
	return 0, nil
}
func (s *proxyRepoStub) CountExpiringSoon(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

type redeemRepoStub struct {
	deleteErrByID map[int64]error
	deletedIDs    []int64

	batchUpdateIDs    []int64
	batchUpdateFields RedeemCodeBatchUpdateFields
	batchUpdateResult int64
	batchUpdateErr    error
	batchUpdateCalled bool
}

func (s *redeemRepoStub) Create(ctx context.Context, code *RedeemCode) error {
	panic("unexpected Create call")
}

func (s *redeemRepoStub) CreateBatch(ctx context.Context, codes []RedeemCode) error {
	panic("unexpected CreateBatch call")
}

func (s *redeemRepoStub) GetByID(ctx context.Context, id int64) (*RedeemCode, error) {
	panic("unexpected GetByID call")
}

func (s *redeemRepoStub) GetByCode(ctx context.Context, code string) (*RedeemCode, error) {
	panic("unexpected GetByCode call")
}

func (s *redeemRepoStub) Update(ctx context.Context, code *RedeemCode) error {
	panic("unexpected Update call")
}

func (s *redeemRepoStub) BatchUpdate(ctx context.Context, ids []int64, fields RedeemCodeBatchUpdateFields) (int64, error) {
	s.batchUpdateCalled = true
	s.batchUpdateIDs = append([]int64(nil), ids...)
	s.batchUpdateFields = fields
	if s.batchUpdateErr != nil {
		return 0, s.batchUpdateErr
	}
	if s.batchUpdateResult != 0 {
		return s.batchUpdateResult, nil
	}
	return int64(len(ids)), nil
}

func (s *redeemRepoStub) Delete(ctx context.Context, id int64) error {
	s.deletedIDs = append(s.deletedIDs, id)
	if s.deleteErrByID != nil {
		if err, ok := s.deleteErrByID[id]; ok {
			return err
		}
	}
	return nil
}

func (s *redeemRepoStub) Use(ctx context.Context, id, userID int64) error {
	panic("unexpected Use call")
}

func (s *redeemRepoStub) List(ctx context.Context, params pagination.PaginationParams) ([]RedeemCode, *pagination.PaginationResult, error) {
	panic("unexpected List call")
}

func (s *redeemRepoStub) ListWithFilters(ctx context.Context, params pagination.PaginationParams, codeType, status, search string) ([]RedeemCode, *pagination.PaginationResult, error) {
	panic("unexpected ListWithFilters call")
}

func (s *redeemRepoStub) ListByUser(ctx context.Context, userID int64, limit int) ([]RedeemCode, error) {
	panic("unexpected ListByUser call")
}

func (s *redeemRepoStub) ListByUserPaginated(ctx context.Context, userID int64, params pagination.PaginationParams, codeType string) ([]RedeemCode, *pagination.PaginationResult, error) {
	panic("unexpected ListByUserPaginated call")
}

func (s *redeemRepoStub) SumPositiveBalanceByUser(ctx context.Context, userID int64) (float64, error) {
	panic("unexpected SumPositiveBalanceByUser call")
}
