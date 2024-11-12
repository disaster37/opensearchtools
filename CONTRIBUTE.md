# Contribute

PR are always welcome.

Please start from branch `2.x`

## CI

### Run all step in local

```bash
dagger call --src . ci export --path .
```

### Format code

```bash
dagger call --src . format export --path .
```

### Run lint

```bash
dagger call --src . lint
```

### Run tests

```bash
dagger call --src . test
```

### Build Docker image

```bash
dagger call --src . build-image
```