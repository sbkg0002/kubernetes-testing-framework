# ktf — Kubernetes Testing Framework

A CLI tool that deploys Kubernetes resources, waits for them to become ready, runs a suite of tests against them, and tears everything down — all driven by a single YAML file.

## Installation

```sh
go install github.com/sbkg0002/kubernetes-testing-framework/cmd/ktf@latest
```

Or build from source:

```sh
make build          # produces bin/ktf
```

## Quick start

```sh
# Validate a suite file without touching the cluster
ktf validate --config example/simple-service.yaml

# Run a suite against the current kubeconfig context
ktf run --config example/simple-service.yaml
```

## How it works

Each `ktf run` goes through four phases in order:

```
apply resources → wait for readiness → run tests → teardown
```

Every phase is context-cancelled, so `--timeout` applies end-to-end across all four phases. Teardown always gets its own fresh 2-minute context so it runs even when the main timeout fires.

## Architecture

[Architecure diagram](docs/architecture.md).

## Suite file reference

```yaml
name: my-service-test # required; used in report output
timeout: 10m # overall deadline (default: 10m)

resources:
  - type: manifest # apply raw YAML manifests
    path: ./k8s/ # file or directory; relative to this file

  - type: helm # install / upgrade a Helm chart
    chart: ./chart/
    values: ./values.test.yaml
    name: my-release # Helm release name (defaults to chart dir name)
    namespace: staging # optional namespace override

wait:
  min: 5s # minimum pause between readiness checks
  max: 2m # cap on exponential backoff
  strategy: backoff # backoff (default) | fixed | linear

tests:
  - name: "API returns 200"
    runner: http
    url: http://my-service.default.svc/healthz
    expect:
      status: 200 # assert HTTP status code
      body: "ok" # optional: assert response body contains string

  - name: "migration ran"
    runner: shell
    script: ./tests/check-migration.sh # path relative to suite file
    env:
      DB_HOST: postgres.default.svc

  - name: "replica count"
    runner: shell
    inline: | # inline bash; non-zero exit = failure
      kubectl get deployment my-service \
        -o jsonpath='{.status.readyReplicas}' | grep -q 3

teardown: always # always (default) | on-success | never
```

### Resource types

| `type`     | What it does                                                                                                                                                       |
| ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `manifest` | Server-side apply of every `.yaml`/`.yml`/`.json` file at `path`. Directories are walked one level deep and sorted by kind (Namespace → CRD → RBAC → Deployments). |
| `helm`     | `helm install` on first run, `helm upgrade` on subsequent runs.                                                                                                    |

### Wait strategies

| `strategy` | Behaviour                                          |
| ---------- | -------------------------------------------------- |
| `backoff`  | Exponential: `min × 2ⁿ`, capped at `max`. Default. |
| `fixed`    | Constant `min` between checks.                     |
| `linear`   | Linear ramp from `min` to `max` over ~10 attempts. |

All strategies add up to 25% random jitter to avoid thundering-herd issues in CI.

**Readiness rules:**

- `Deployment`, `StatefulSet`, `DaemonSet` — `.status.readyReplicas >= .spec.replicas`
- `Job` — condition `Complete=True`
- Everything else (`Service`, `ConfigMap`, `Secret`, …) — considered ready immediately

### Runners

| `runner` | Fields                                                                            |
| -------- | --------------------------------------------------------------------------------- |
| `http`   | `url`, `expect.status`, `expect.body` (substring match)                           |
| `shell`  | `script` (path relative to suite file) **or** `inline` (bash string), `env` (map) |

Third-party runners can register themselves before the engine starts:

```go
runner.Register("grpc", func(tc config.TestCase) (runner.Runner, error) {
    return &myGRPCRunner{tc: tc}, nil
})
```

### Teardown modes

| `teardown`   | When resources are deleted                  |
| ------------ | ------------------------------------------- |
| `always`     | After every run, regardless of test outcome |
| `on-success` | Only when all tests pass                    |
| `never`      | Resources are left in the cluster           |

## CLI reference

```
ktf run      --config <file>  [--kubeconfig <file>] [--namespace <ns>]
             [--output pretty|json] [--parallel] [--timeout <duration>]

ktf validate --config <file>  # lint + semantic check, no cluster access
```

All flags can be set via environment variables with the `KTF_` prefix:

```sh
KTF_CONFIG=./suites/smoke.yaml KTF_OUTPUT=json ktf run
```

Exit code is `0` when all tests pass, `1` when any test fails or a phase errors.

## Output formats

**`--output pretty`** (default)

```
Suite:  my-service-test
────────────────────────────────────────────────────────────

  PASS  API returns 200                          (87ms)
  FAIL  migration ran                            (1.3s)
        exit: exit status 1
        psql: connection refused

────────────────────────────────────────────────────────────
  FAIL  1/2 passed  (3.4s)
```

**`--output json`**

```json
{
  "name": "my-service-test",
  "total": 2,
  "passed": 1,
  "failed": 1,
  "duration_ms": 3400000000,
  "results": [
    { "name": "API returns 200", "passed": true, "elapsed_ms": 87000000 },
    { "name": "migration ran", "passed": false, "output": "exit: exit status 1\n...", "elapsed_ms": 1300000000 }
  ]
}
```

Set `NO_COLOR=1` to suppress ANSI codes in pretty output.

## Example

The `example/simple-service/` directory contains manifests for [traefik/whoami](https://github.com/traefik/whoami) (a 2-replica Deployment + ClusterIP Service). A matching suite is at `example/simple-service.yaml`.

```sh
# spin up a local cluster
make cluster-up

# run the example suite
ktf run --config example/simple-service.yaml

# clean up
make cluster-down
```

## Development

```sh
make build            # compile bin/ktf
make test             # go test ./...
make lint             # golangci-lint (requires golangci-lint installed)
make cluster-up       # kind create cluster --name ktf-dev
make integration-test # go test ./... -tags integration
```
