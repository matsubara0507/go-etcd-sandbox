package lock

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pkg/errors"
	"go.etcd.io/etcd/server/v3/embed"
	"go.etcd.io/etcd/server/v3/etcdserver"
)

func TestEtcdUserDatabase_UpdateUser(t *testing.T) {
	server, err := startEmbedEtcdUser(".", 8888)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

	t.Run("single", func(t *testing.T) {
		db1, err := NewEtcdUserDatabase("localhost:8888")
		if err != nil {
			t.Fatal(err)
		}
		if err := db1.SetUser(context.Background(), &User{1, "hige"}); err != nil {
			t.Fatal(err)
		}

		err = db1.UpdateUser(context.Background(), 1, 10, func(user *User) bool {
			user.Name = "higege"
			return true
		})
		if err != nil {
			t.Error(err)
		}

		user, err := db1.GetUser(context.Background(), 1)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.EqualFold(user.Name, "higege") {
			t.Errorf("unexpected name: %s", user.Name)
		}
	})

	t.Run("double: success", func(t *testing.T) {
		db1, err := NewEtcdUserDatabase("localhost:8888")
		if err != nil {
			t.Fatal(err)
		}
		if err := db1.SetUser(context.Background(), &User{2, "hige"}); err != nil {
			t.Fatal(err)
		}

		db2, err := NewEtcdUserDatabase("localhost:8888")
		if err != nil {
			t.Fatal(err)
		}

		wg := sync.WaitGroup{}
		for _, v := range []*EtcdUserDatabase{db1, db2} {
			wg.Add(1)
			go func(db *EtcdUserDatabase) {
				defer wg.Done()
				err := db.UpdateUser(context.Background(), 2, 10, func(user *User) bool {
					user.Name += "ge"
					return true
				})
				if err != nil {
					t.Error(err)
				}
			}(v)
		}
		wg.Wait()

		user, err := db1.GetUser(context.Background(), 2)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.EqualFold(user.Name, "higegege") {
			t.Errorf("unexpected name: %s", user.Name)
		}
	})

	t.Run("double: failure", func(t *testing.T) {
		db1, err := NewEtcdUserDatabase("localhost:8888")
		if err != nil {
			t.Fatal(err)
		}
		if err := db1.SetUser(context.Background(), &User{3, "hige"}); err != nil {
			t.Fatal(err)
		}

		db2, err := NewEtcdUserDatabase("localhost:8888")
		if err != nil {
			t.Fatal(err)
		}

		var wg, wg1, wg2 sync.WaitGroup
		wg.Add(1)
		wg1.Add(1)
		wg2.Add(1)

		go func() {
			defer wg.Done()
			err := db1.UpdateUser(context.Background(), 3, 3, func(user *User) bool {
				wg1.Done()
				user.Name += "ge"
				wg2.Wait()
				return true
			})
			if err != nil {
				t.Error(err)
			}
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			wg1.Wait()
			err := db2.UpdateUser(context.Background(), 3, 10, func(user *User) bool {
				user.Name += "ge"
				wg2.Done()
				return true
			})
			if err != nil {
				t.Error(err)
			}
		}()

		wg.Wait()

		user, err := db1.GetUser(context.Background(), 3)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.EqualFold(user.Name, "higege") {
			t.Errorf("unexpected name: %s", user.Name)
		}
	})

	t.Run("double: use SafeUpdateUser", func(t *testing.T) {
		db1, err := NewEtcdUserDatabase("localhost:8888")
		if err != nil {
			t.Fatal(err)
		}
		if err := db1.SetUser(context.Background(), &User{4, "hige"}); err != nil {
			t.Fatal(err)
		}

		db2, err := NewEtcdUserDatabase("localhost:8888")
		if err != nil {
			t.Fatal(err)
		}

		var wg, wg1, wg2 sync.WaitGroup
		wg.Add(1)
		wg1.Add(1)
		wg2.Add(1)

		go func() {
			defer wg.Done()
			err := db1.SafeUpdateUser(context.Background(), 4, 3, func(user *User) bool {
				wg1.Done()
				user.Name += "ge"
				wg2.Wait()
				return true
			})
			if err == nil {
				t.Error("unexpected error")
			}
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			wg1.Wait()
			err := db2.SafeUpdateUser(context.Background(), 4, 10, func(user *User) bool {
				user.Name += "ge"
				wg2.Done()
				return true
			})
			if err != nil {
				t.Error(err)
			}
		}()

		wg.Wait()

		user, err := db1.GetUser(context.Background(), 4)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.EqualFold(user.Name, "higege") {
			t.Errorf("unexpected name: %s", user.Name)
		}
	})
}

func startEmbedEtcdUser(dir string, port int) (*etcdserver.EtcdServer, error) {
	peerPort := unusedPort(10000)
	cfg := embed.NewConfig()
	cfg.LogLevel = "error"
	cfg.Dir = filepath.Join(dir, "default.etcd")
	cfg.LCUrls[0].Host = fmt.Sprintf("localhost:%d", port)
	cfg.LPUrls[0].Host = fmt.Sprintf("localhost:%d", peerPort)
	e, err := embed.StartEtcd(cfg)
	if err != nil {
		return nil, err
	}

	select {
	case <-e.Server.ReadyNotify():
	case <-time.After(10 * time.Second):
		e.Server.Stop()
		return nil, errors.New("failed start etcd")
	}
	return e.Server, nil
}

func unusedPort(base int) int {
	for i := 0; i < 3; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(10000))
		if err != nil {
			continue
		}
		port := base + int(n.Int64())
		l, err := net.Listen("tcp4", fmt.Sprintf(":%d", port))
		if err == nil {
			l.Close()
			return port
		}
	}

	return -1
}
