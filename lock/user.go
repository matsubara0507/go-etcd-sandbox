package lock

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"
	"go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"
	"gopkg.in/yaml.v2"
)

var (
	ErrNotFound = errors.New("user not found")
)

type User struct {
	Id   int    `yaml:"id"`
	Name string `yaml:"name"`
}

type EtcdUserDatabase struct {
	backend *clientv3.Client
}

func NewEtcdUserDatabase(endpoint string) (*EtcdUserDatabase, error) {
	c, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{endpoint},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	return &EtcdUserDatabase{c}, nil
}

func (db *EtcdUserDatabase) key(id int) string {
	return fmt.Sprintf("users/%d/", id)
}

func (db *EtcdUserDatabase) GetUser(ctx context.Context, id int) (*User, error) {
	resp, err := db.backend.Get(ctx, db.key(id))
	if err != nil {
		return nil, err
	}
	if resp.Count == 0 {
		return nil, ErrNotFound
	}
	var user User
	if err := yaml.Unmarshal(resp.Kvs[0].Value, &user); err != nil {
		return nil, err
	}
	return &user, nil
}

func (db *EtcdUserDatabase) SetUser(ctx context.Context, user *User) error {
	b, err := yaml.Marshal(user)
	if err != nil {
		return err
	}
	if _, err := db.backend.Put(ctx, db.key(user.Id), string(b)); err != nil {
		return err
	}
	return nil
}

func (db *EtcdUserDatabase) UpdateUser(ctx context.Context, id int, ttl int, update func(*User) bool) error {
	session, err := concurrency.NewSession(db.backend, concurrency.WithTTL(ttl))
	if err != nil {
		return err
	}
	defer session.Close()
	session.Orphan()

	mu := concurrency.NewMutex(session, "users_update/mutex")
	if err := mu.Lock(ctx); err != nil {
		return err
	}
	defer mu.Unlock(ctx)

	user, err := db.GetUser(ctx, id)
	if err != nil {
		return err
	}
	if update(user) {
		if err := db.SetUser(ctx, user); err != nil {
			return err
		}
	}
	return nil
}

func (db *EtcdUserDatabase) SafeUpdateUser(ctx context.Context, id int, ttl int, update func(*User) bool) error {
	session, err := concurrency.NewSession(db.backend, concurrency.WithTTL(ttl))
	if err != nil {
		return err
	}
	defer session.Close()
	session.Orphan()

	mu := concurrency.NewMutex(session, "users_update/mutex")
	if err := mu.Lock(ctx); err != nil {
		return err
	}
	defer mu.Unlock(ctx)

	user, err := db.GetUser(ctx, id)
	if err != nil {
		return err
	}
	if update(user) {
		b, err := yaml.Marshal(user)
		if err != nil {
			return err
		}
		resp, err := db.backend.Txn(ctx).
			If(mu.IsOwner()).
			Then(clientv3.OpPut(db.key(user.Id), string(b))).
			Commit()
		if err != nil {
			return err
		}
		if !resp.Succeeded {
			return errors.New("failure commit")
		}
	}
	return nil
}
