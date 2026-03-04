package lock

import (
	"fmt"
	"sync"
	"time"

	"distributed-chat-coordinator/internal/types"
)

const (
	DefaultLeaseDuration = 10 * time.Second
	LeaseCleanupInterval = 1 * time.Second
)

type Lease struct {
	Resource  string    `json:"resource"`
	Holder    string    `json:"holder"`
	GrantedAt time.Time `json:"granted_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Duration  time.Duration `json:"duration"`
}

func (l *Lease) IsExpired() bool {
	return time.Now().After(l.ExpiresAt)
}

type LockManager struct {
	mu     sync.Mutex
	nodeID int
	prefix string

	leases map[string]*Lease

	leaseDuration time.Duration

	stopCh chan struct{}
}

func NewLockManager(nodeID int, leaseDuration time.Duration) *LockManager {
	if leaseDuration <= 0 {
		leaseDuration = DefaultLeaseDuration
	}
	return &LockManager{
		nodeID:        nodeID,
		prefix:        types.LogPrefix(nodeID, "LOCK"),
		leases:        make(map[string]*Lease),
		leaseDuration: leaseDuration,
		stopCh:        make(chan struct{}),
	}
}

func (lm *LockManager) Start() {
	go lm.cleanupLoop()
	fmt.Printf("%s🔓 Lock manager started (lease TTL: %v)\n", lm.prefix, lm.leaseDuration)
}

func (lm *LockManager) Stop() {
	close(lm.stopCh)
}

func (lm *LockManager) Acquire(resource, requester string) types.LockResponse {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	now := time.Now()

	if existing, ok := lm.leases[resource]; ok {
		if !existing.IsExpired() {
			fmt.Printf("%s🚫 DENIED lock on \"%s\" to [%s] — held by [%s] until %s\n",
				lm.prefix, resource, requester, existing.Holder,
				existing.ExpiresAt.Format("15:04:05.000"))
			return types.LockResponse{
				Resource: resource,
				Granted:  false,
				Holder:   existing.Holder,
				ExpiresAt: existing.ExpiresAt,
				Reason:   fmt.Sprintf("resource locked by %s until %s", existing.Holder, existing.ExpiresAt.Format("15:04:05")),
			}
		}
		fmt.Printf("%s⏰ Previous lease on \"%s\" by [%s] has expired. Revoking.\n",
			lm.prefix, resource, existing.Holder)
		delete(lm.leases, resource)
	}

	lease := &Lease{
		Resource:  resource,
		Holder:    requester,
		GrantedAt: now,
		ExpiresAt: now.Add(lm.leaseDuration),
		Duration:  lm.leaseDuration,
	}
	lm.leases[resource] = lease

	fmt.Printf("%s✅ GRANTED lock on \"%s\" to [%s] (expires: %s)\n",
		lm.prefix, resource, requester,
		lease.ExpiresAt.Format("15:04:05.000"))

	return types.LockResponse{
		Resource:  resource,
		Granted:   true,
		Holder:    requester,
		ExpiresAt: lease.ExpiresAt,
		Reason:    "lock acquired successfully",
	}
}

func (lm *LockManager) Release(resource, requester string) bool {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	lease, ok := lm.leases[resource]
	if !ok {
		fmt.Printf("%s⚠️  Cannot release \"%s\" — no active lease\n",
			lm.prefix, resource)
		return false
	}

	if lease.Holder != requester {
		fmt.Printf("%s⚠️  Cannot release \"%s\" — held by [%s], not [%s]\n",
			lm.prefix, resource, lease.Holder, requester)
		return false
	}

	delete(lm.leases, resource)
	fmt.Printf("%s🔓 Released lock on \"%s\" by [%s]\n",
		lm.prefix, resource, requester)
	return true
}

func (lm *LockManager) GetLease(resource string) *Lease {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	return lm.leases[resource]
}

func (lm *LockManager) GetAllLeases() map[string]*Lease {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	result := make(map[string]*Lease)
	for k, v := range lm.leases {
		if !v.IsExpired() {
			result[k] = v
		}
	}
	return result
}

func (lm *LockManager) cleanupLoop() {
	ticker := time.NewTicker(LeaseCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-lm.stopCh:
			return
		case <-ticker.C:
			lm.cleanupExpired()
		}
	}
}

func (lm *LockManager) cleanupExpired() {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	for resource, lease := range lm.leases {
		if lease.IsExpired() {
			fmt.Printf("%s⏰ Auto-expired lease on \"%s\" (was held by [%s])\n",
				lm.prefix, resource, lease.Holder)
			delete(lm.leases, resource)
		}
	}
}
