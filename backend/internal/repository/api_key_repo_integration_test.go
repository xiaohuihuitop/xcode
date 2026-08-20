//go:build integration

package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type APIKeyRepoSuite struct {
	suite.Suite
	ctx    context.Context
	client *dbent.Client
	repo   *apiKeyRepository
}

func (s *APIKeyRepoSuite) SetupTest() {
	s.ctx = context.Background()
	tx := testEntTx(s.T())
	s.client = tx.Client()
	s.repo = newAPIKeyRepositoryWithSQL(s.client, tx)
}

func TestAPIKeyRepoSuite(t *testing.T) {
	suite.Run(t, new(APIKeyRepoSuite))
}

// --- Create / GetByID / GetByKey ---

func (s *APIKeyRepoSuite) TestCreate() {
	user := s.mustCreateUser("create@test.com")

	key := &service.APIKey{
		UserID: user.ID,
		Key:    "sk-create-test",
		Name:   "Test Key",
		Status: service.StatusActive,
	}

	err := s.repo.Create(s.ctx, key)
	s.Require().NoError(err, "Create")
	s.Require().NotZero(key.ID, "expected ID to be set")

	got, err := s.repo.GetByID(s.ctx, key.ID)
	s.Require().NoError(err, "GetByID")
	s.Require().Equal("sk-create-test", got.Key)
}

func (s *APIKeyRepoSuite) TestGetByID_NotFound() {
	_, err := s.repo.GetByID(s.ctx, 999999)
	s.Require().Error(err, "expected error for non-existent ID")
}

func (s *APIKeyRepoSuite) TestGetByKey() {
	user := s.mustCreateUser("getbykey@test.com")

	key := &service.APIKey{
		UserID: user.ID,
		Key:    "sk-getbykey",
		Name:   "My Key",
		Status: service.StatusActive,
	}
	s.Require().NoError(s.repo.Create(s.ctx, key))

	got, err := s.repo.GetByKey(s.ctx, key.Key)
	s.Require().NoError(err, "GetByKey")
	s.Require().Equal(key.ID, got.ID)
	s.Require().NotNil(got.User, "expected User preload")
	s.Require().Equal(user.ID, got.User.ID)
}

func (s *APIKeyRepoSuite) TestGetByKey_NotFound() {
	_, err := s.repo.GetByKey(s.ctx, "non-existent-key")
	s.Require().Error(err, "expected error for non-existent key")
}

// --- Update ---

func (s *APIKeyRepoSuite) TestUpdate() {
	user := s.mustCreateUser("update@test.com")
	key := &service.APIKey{
		UserID: user.ID,
		Key:    "sk-update",
		Name:   "Original",
		Status: service.StatusActive,
	}
	s.Require().NoError(s.repo.Create(s.ctx, key))

	key.Name = "Renamed"
	key.Status = service.StatusDisabled
	err := s.repo.Update(s.ctx, key, service.APIKeyUpdateFields{Name: true, Status: true})
	s.Require().NoError(err, "Update")

	got, err := s.repo.GetByID(s.ctx, key.ID)
	s.Require().NoError(err, "GetByID after update")
	s.Require().Equal("sk-update", got.Key, "Update should not change key")
	s.Require().Equal(user.ID, got.UserID, "Update should not change user_id")
	s.Require().Equal("Renamed", got.Name)
	s.Require().Equal(service.StatusDisabled, got.Status)
}

// --- Delete ---

func (s *APIKeyRepoSuite) TestDelete() {
	user := s.mustCreateUser("delete@test.com")
	key := &service.APIKey{
		UserID: user.ID,
		Key:    "sk-delete",
		Name:   "Delete Me",
		Status: service.StatusActive,
	}
	s.Require().NoError(s.repo.Create(s.ctx, key))

	err := s.repo.Delete(s.ctx, key.ID)
	s.Require().NoError(err, "Delete")

	_, err = s.repo.GetByID(s.ctx, key.ID)
	s.Require().Error(err, "expected error after delete")
}

func (s *APIKeyRepoSuite) TestCreate_AfterSoftDelete_AllowsSameKey() {
	user := s.mustCreateUser("recreate-after-soft-delete@test.com")
	const reusedKey = "sk-reuse-after-soft-delete"

	first := &service.APIKey{
		UserID: user.ID,
		Key:    reusedKey,
		Name:   "First Key",
		Status: service.StatusActive,
	}
	s.Require().NoError(s.repo.Create(s.ctx, first), "create first key")

	s.Require().NoError(s.repo.Delete(s.ctx, first.ID), "soft delete first key")

	second := &service.APIKey{
		UserID: user.ID,
		Key:    reusedKey,
		Name:   "Second Key",
		Status: service.StatusActive,
	}
	s.Require().NoError(s.repo.Create(s.ctx, second), "create second key with same key")
	s.Require().NotZero(second.ID)
	s.Require().NotEqual(first.ID, second.ID, "recreated key should be a new row")
}

// --- ListByUserID / CountByUserID ---

func (s *APIKeyRepoSuite) TestListByUserID() {
	user := s.mustCreateUser("listbyuser@test.com")
	s.mustCreateApiKey(user.ID, "sk-list-1", "Key 1")
	s.mustCreateApiKey(user.ID, "sk-list-2", "Key 2")

	keys, page, err := s.repo.ListByUserID(s.ctx, user.ID, pagination.PaginationParams{Page: 1, PageSize: 10}, service.APIKeyListFilters{})
	s.Require().NoError(err, "ListByUserID")
	s.Require().Len(keys, 2)
	s.Require().Equal(int64(2), page.Total)
}

func (s *APIKeyRepoSuite) TestListByUserID_Pagination() {
	user := s.mustCreateUser("paging@test.com")
	for i := 0; i < 5; i++ {
		s.mustCreateApiKey(user.ID, "sk-page-"+string(rune('a'+i)), "Key")
	}

	keys, page, err := s.repo.ListByUserID(s.ctx, user.ID, pagination.PaginationParams{Page: 1, PageSize: 2}, service.APIKeyListFilters{})
	s.Require().NoError(err)
	s.Require().Len(keys, 2)
	s.Require().Equal(int64(5), page.Total)
	s.Require().Equal(3, page.Pages)
}

func (s *APIKeyRepoSuite) TestCountByUserID() {
	user := s.mustCreateUser("count@test.com")
	s.mustCreateApiKey(user.ID, "sk-count-1", "K1")
	s.mustCreateApiKey(user.ID, "sk-count-2", "K2")

	count, err := s.repo.CountByUserID(s.ctx, user.ID)
	s.Require().NoError(err, "CountByUserID")
	s.Require().Equal(int64(2), count)
}

// --- ExistsByKey ---

func (s *APIKeyRepoSuite) TestExistsByKey() {
	user := s.mustCreateUser("exists@test.com")
	s.mustCreateApiKey(user.ID, "sk-exists", "K")

	exists, err := s.repo.ExistsByKey(s.ctx, "sk-exists")
	s.Require().NoError(err, "ExistsByKey")
	s.Require().True(exists)

	notExists, err := s.repo.ExistsByKey(s.ctx, "sk-not-exists")
	s.Require().NoError(err)
	s.Require().False(notExists)
}

// --- SearchAPIKeys ---

func (s *APIKeyRepoSuite) TestSearchAPIKeys() {
	user := s.mustCreateUser("search@test.com")
	s.mustCreateApiKey(user.ID, "sk-search-1", "Production Key")
	s.mustCreateApiKey(user.ID, "sk-search-2", "Development Key")

	found, err := s.repo.SearchAPIKeys(s.ctx, user.ID, "prod", 10)
	s.Require().NoError(err, "SearchAPIKeys")
	s.Require().Len(found, 1)
	s.Require().Contains(found[0].Name, "Production")
}

func (s *APIKeyRepoSuite) TestSearchAPIKeys_NoKeyword() {
	user := s.mustCreateUser("searchnokw@test.com")
	s.mustCreateApiKey(user.ID, "sk-nk-1", "K1")
	s.mustCreateApiKey(user.ID, "sk-nk-2", "K2")

	found, err := s.repo.SearchAPIKeys(s.ctx, user.ID, "", 10)
	s.Require().NoError(err)
	s.Require().Len(found, 2)
}

func (s *APIKeyRepoSuite) TestSearchAPIKeys_NoUserID() {
	user := s.mustCreateUser("searchnouid@test.com")
	s.mustCreateApiKey(user.ID, "sk-nu-1", "TestKey")

	found, err := s.repo.SearchAPIKeys(s.ctx, 0, "testkey", 10)
	s.Require().NoError(err)
	s.Require().Len(found, 1)
}

func (s *APIKeyRepoSuite) mustCreateUser(email string) *service.User {
	s.T().Helper()

	u, err := s.client.User.Create().
		SetEmail(email).
		SetPasswordHash("test-password-hash").
		SetStatus(service.StatusActive).
		SetRole(service.RoleUser).
		Save(s.ctx)
	s.Require().NoError(err, "create user")
	return userEntityToService(u)
}

func (s *APIKeyRepoSuite) mustCreateApiKey(userID int64, key, name string) *service.APIKey {
	s.T().Helper()

	k := &service.APIKey{
		UserID: userID,
		Key:    key,
		Name:   name,
		Status: service.StatusActive,
	}
	s.Require().NoError(s.repo.Create(s.ctx, k), "create api key")
	return k
}

// --- IncrementQuotaUsed ---

func (s *APIKeyRepoSuite) TestIncrementQuotaUsed_Basic() {
	user := s.mustCreateUser("incr-basic@test.com")
	key := s.mustCreateApiKey(user.ID, "sk-incr-basic", "Incr")

	newQuota, err := s.repo.IncrementQuotaUsed(s.ctx, key.ID, 1.5)
	s.Require().NoError(err, "IncrementQuotaUsed")
	s.Require().Equal(1.5, newQuota, "第一次递增后应为 1.5")

	newQuota, err = s.repo.IncrementQuotaUsed(s.ctx, key.ID, 2.5)
	s.Require().NoError(err, "IncrementQuotaUsed second")
	s.Require().Equal(4.0, newQuota, "第二次递增后应为 4.0")
}

func (s *APIKeyRepoSuite) TestIncrementQuotaUsed_NotFound() {
	_, err := s.repo.IncrementQuotaUsed(s.ctx, 999999, 1.0)
	s.Require().ErrorIs(err, service.ErrAPIKeyNotFound, "不存在的 key 应返回 ErrAPIKeyNotFound")
}

func (s *APIKeyRepoSuite) TestIncrementQuotaUsed_DeletedKey() {
	user := s.mustCreateUser("incr-deleted@test.com")
	key := s.mustCreateApiKey(user.ID, "sk-incr-del", "Deleted")

	s.Require().NoError(s.repo.Delete(s.ctx, key.ID), "Delete")

	_, err := s.repo.IncrementQuotaUsed(s.ctx, key.ID, 1.0)
	s.Require().ErrorIs(err, service.ErrAPIKeyNotFound, "已删除的 key 应返回 ErrAPIKeyNotFound")
}

func (s *APIKeyRepoSuite) TestIncrementQuotaUsedAndGetState() {
	user := s.mustCreateUser("quota-state@test.com")
	key := s.mustCreateApiKey(user.ID, "sk-quota-state", "QuotaState")
	key.Quota = 3
	key.QuotaUsed = 1
	s.Require().NoError(s.repo.Update(s.ctx, key, service.APIKeyUpdateFields{Quota: true, QuotaUsed: true}), "Update quota")

	state, err := s.repo.IncrementQuotaUsedAndGetState(s.ctx, key.ID, 2.5)
	s.Require().NoError(err, "IncrementQuotaUsedAndGetState")
	s.Require().NotNil(state)
	s.Require().Equal(3.5, state.QuotaUsed)
	s.Require().Equal(3.0, state.Quota)
	s.Require().Equal(service.StatusAPIKeyQuotaExhausted, state.Status)
	s.Require().Equal(key.Key, state.Key)

	got, err := s.repo.GetByID(s.ctx, key.ID)
	s.Require().NoError(err, "GetByID")
	s.Require().Equal(3.5, got.QuotaUsed)
	s.Require().Equal(service.StatusAPIKeyQuotaExhausted, got.Status)
}

// TestIncrementQuotaUsed_Concurrent 使用真实数据库验证并发原子性。
// 注意：此测试使用 testEntClient（非事务隔离），数据会真正写入数据库。
func TestIncrementQuotaUsed_Concurrent(t *testing.T) {
	client := testEntClient(t)
	repo := NewAPIKeyRepository(client, integrationDB).(*apiKeyRepository)
	ctx := context.Background()

	// 创建测试用户和 API Key
	u, err := client.User.Create().
		SetEmail("concurrent-incr-" + time.Now().Format(time.RFC3339Nano) + "@test.com").
		SetPasswordHash("hash").
		SetStatus(service.StatusActive).
		SetRole(service.RoleUser).
		Save(ctx)
	require.NoError(t, err, "create user")

	k := &service.APIKey{
		UserID: u.ID,
		Key:    "sk-concurrent-" + time.Now().Format(time.RFC3339Nano),
		Name:   "Concurrent",
		Status: service.StatusActive,
	}
	require.NoError(t, repo.Create(ctx, k), "create api key")
	t.Cleanup(func() {
		_ = client.APIKey.DeleteOneID(k.ID).Exec(ctx)
		_ = client.User.DeleteOneID(u.ID).Exec(ctx)
	})

	// 10 个 goroutine 各递增 1.0，总计应为 10.0
	const goroutines = 10
	const increment = 1.0
	var wg sync.WaitGroup
	errs := make([]error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, errs[idx] = repo.IncrementQuotaUsed(ctx, k.ID, increment)
		}(i)
	}
	wg.Wait()

	for i, e := range errs {
		require.NoError(t, e, "goroutine %d failed", i)
	}

	// 验证最终结果
	got, err := repo.GetByID(ctx, k.ID)
	require.NoError(t, err, "GetByID")
	require.Equal(t, float64(goroutines)*increment, got.QuotaUsed,
		"并发递增后总和应为 %v，实际为 %v", float64(goroutines)*increment, got.QuotaUsed)
}

func (s *APIKeyRepoSuite) TestDeleteWithAudit_TombstonesWithoutRetainingCredential() {
	user := s.mustCreateUser("delwithaudit@test.com")
	key := &service.APIKey{
		UserID: user.ID,
		Key:    "sk-del-audit-1",
		Name:   "Audit Me",
		Status: service.StatusActive,
	}
	s.Require().NoError(s.repo.Create(s.ctx, key))

	s.Require().NoError(s.repo.DeleteWithAudit(s.ctx, key.ID))

	_, err := s.repo.GetByID(s.ctx, key.ID)
	s.Require().Error(err)

	var tombstone string
	var deletedAt time.Time
	rows, err := s.repo.sql.QueryContext(s.ctx, `SELECT key, deleted_at FROM api_keys WHERE id = $1`, key.ID)
	s.Require().NoError(err)
	s.Require().True(rows.Next())
	s.Require().NoError(rows.Scan(&tombstone, &deletedAt))
	s.Require().NoError(rows.Close())
	s.Require().NotEqual("sk-del-audit-1", tombstone)
	s.Require().Contains(tombstone, "__deleted__")

	var auditCount int
	auditRows, err := s.repo.sql.QueryContext(s.ctx,
		`SELECT COUNT(*) FROM deleted_api_key_audits WHERE api_key_id = $1`, key.ID)
	s.Require().NoError(err)
	s.Require().True(auditRows.Next())
	s.Require().NoError(auditRows.Scan(&auditCount))
	s.Require().NoError(auditRows.Close())
	s.Require().Zero(auditCount, "deleted credentials must not be retained")
}

func (s *APIKeyRepoSuite) TestDeleteWithAudit_RepeatIsIdempotent() {
	user := s.mustCreateUser("delwithaudit-idem@test.com")
	key := &service.APIKey{UserID: user.ID, Key: "sk-del-audit-2", Name: "K", Status: service.StatusActive}
	s.Require().NoError(s.repo.Create(s.ctx, key))

	s.Require().NoError(s.repo.DeleteWithAudit(s.ctx, key.ID))
	s.Require().NoError(s.repo.DeleteWithAudit(s.ctx, key.ID))
}

func (s *APIKeyRepoSuite) TestDeleteWithAudit_NotFound() {
	err := s.repo.DeleteWithAudit(s.ctx, 999999)
	s.Require().ErrorIs(err, service.ErrAPIKeyNotFound)
}
