package bench

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// homeDir is a task's directory of files for its fake home.
const homeDir = "home"

// Fixture is what a run of a task needs besides its workspace: the
// variables the setup, the harness, and the check see, and a service
// running in the background. Stop ends it.
type Fixture struct {
	// Env holds TASK_DIR, and SERVICE_URL and HOME when the task has a
	// service or a fake home.
	Env []string
	// URL is the service's address, or "".
	URL string
	// Home is the fake home, or "".
	Home string
	svc  *exec.Cmd
	port int
}

// StartFixture makes the task's fake home under scratch and starts its
// service, waiting until the service accepts connections.
func (t Task) StartFixture(ctx context.Context, scratch string, base []string) (*Fixture, error) {
	abs, err := filepath.Abs(t.Dir)
	if err != nil {
		return nil, err
	}
	fx := &Fixture{Env: []string{"TASK_DIR=" + abs}}
	if t.FakeHome {
		fx.Home = filepath.Join(scratch, "home")
		if err := os.MkdirAll(fx.Home, 0o755); err != nil {
			return nil, err
		}
		if _, err := os.Stat(filepath.Join(t.Dir, homeDir)); err == nil {
			if err := copyTree(filepath.Join(t.Dir, homeDir), fx.Home); err != nil {
				return nil, err
			}
		}
		fx.Env = append(fx.Env, "HOME="+fx.Home)
	}
	if t.Service == "" {
		return fx, nil
	}
	port, err := freePort(ctx)
	if err != nil {
		return nil, err
	}
	fx.port = port
	fx.URL = "http://127.0.0.1:" + strconv.Itoa(port)
	fx.Env = append(fx.Env, "SERVICE_URL="+fx.URL)
	// The service runs outside the run's context, so it outlives nothing
	// but is stopped by Stop.
	svc := exec.CommandContext(context.WithoutCancel(ctx), "sh", "-c", t.Service)
	svc.Dir = abs
	svc.Env = append(append(append([]string{}, base...), fx.Env...), "PORT="+strconv.Itoa(port))
	svc.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	log, err := os.Create(filepath.Join(scratch, "service.log"))
	if err != nil {
		fx.Stop()

		return nil, err
	}
	defer log.Close()
	svc.Stdout, svc.Stderr = log, log
	if err := svc.Start(); err != nil {
		fx.Stop()

		return nil, fmt.Errorf("service: %w", err)
	}
	fx.svc = svc
	if err := waitPort(ctx, port, serviceStart); err != nil {
		fx.Stop()

		return nil, fmt.Errorf("service did not listen on %d: %w (see %s)", port, err, log.Name())
	}

	return fx, nil
}

// serviceStart bounds a service's start, which may compile it first
// (`go run`) with a cold build cache.
const serviceStart = 3 * time.Minute

// Stop ends the service and its process group.
func (fx *Fixture) Stop() {
	if fx == nil {
		return
	}
	if fx.port != 0 {
		releasePort(fx.port)
		fx.port = 0
	}
	if fx.svc == nil || fx.svc.Process == nil {
		return
	}
	_ = syscall.Kill(-fx.svc.Process.Pid, syscall.SIGKILL)
	_ = fx.svc.Wait()
	fx.svc = nil
}

// Expand fills a prompt's or a check's placeholders: {{URL}} is the
// service's address.
func (fx *Fixture) Expand(s string) string {
	return strings.ReplaceAll(s, "{{URL}}", fx.URL)
}

// reserved holds the ports handed to services that have not stopped. A
// service may take minutes to listen (`go run` compiles first), and in that
// time the system may offer its port again; another task's service would
// then answer this task's requests.
var reserved = struct {
	sync.Mutex

	ports map[int]bool
}{ports: map[int]bool{}}

// freePort finds a port no other fixture holds and reserves it until
// releasePort.
func freePort(ctx context.Context) (int, error) {
	var lc net.ListenConfig
	for range 100 {
		l, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
		if err != nil {
			return 0, err
		}
		addr, ok := l.Addr().(*net.TCPAddr)
		_ = l.Close()
		if !ok {
			return 0, errors.New("not a TCP address")
		}
		reserved.Lock()
		taken := reserved.ports[addr.Port]
		if !taken {
			reserved.ports[addr.Port] = true
		}
		reserved.Unlock()
		if !taken {
			return addr.Port, nil
		}
	}

	return 0, errors.New("no free port")
}

func releasePort(port int) {
	reserved.Lock()
	delete(reserved.ports, port)
	reserved.Unlock()
}

func waitPort(ctx context.Context, port int, limit time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	var d net.Dialer
	for {
		c, err := d.DialContext(ctx, "tcp", "127.0.0.1:"+strconv.Itoa(port))
		if err == nil {
			return c.Close()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// gitIdentity lets a task's scripts commit where git has no user configured,
// as on a CI runner. It also turns off git's background maintenance, whose
// lock files come and go while the workspace is copied.
var gitIdentity = []string{
	"GIT_AUTHOR_NAME=agentbench", "GIT_AUTHOR_EMAIL=agentbench@localhost",
	"GIT_COMMITTER_NAME=agentbench", "GIT_COMMITTER_EMAIL=agentbench@localhost",
	"GIT_CONFIG_COUNT=2",
	"GIT_CONFIG_KEY_0=maintenance.auto", "GIT_CONFIG_VALUE_0=false",
	"GIT_CONFIG_KEY_1=gc.auto", "GIT_CONFIG_VALUE_1=0",
}

// runScript runs a task's shell script (its setup or its solution script)
// in ws.
func runScript(ctx context.Context, script, ws string, env []string) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", script)
	cmd.Dir, cmd.Env = ws, slices.Concat(env, gitIdentity)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, tail(string(out), 2000))
	}

	return nil
}
