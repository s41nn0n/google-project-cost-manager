# Contributing

Contributions are welcome. Keep changes small and add tests for behavior changes.

## Local validation

Run the same checks as CI before opening a pull request:

```sh
test -z "$(gofmt -l .)"
go test ./...
go vet ./...
docker build -t billing-guard:ci .
terraform -chdir=deployments/terraform fmt -check -recursive
terraform -chdir=deployments/terraform init -backend=false -input=false
terraform -chdir=deployments/terraform validate
```

If you are not changing deployment files, the Terraform checks are usually only needed as a final validation before submitting.

## Commit messages

Use Conventional Commits so Release Please can prepare changelogs and releases:

- `feat: add a new capability`
- `fix: correct existing behavior`
- `docs: update deployment guidance`
- `chore: update automation`
- `feat!: change configuration format` or a `BREAKING CHANGE:` footer for breaking changes

`feat:` and `fix:` commits affect release versioning. Documentation, chores, and breaking-change notes are included in release PR changelogs when applicable.

## Secrets and safety

Do not commit credentials, service account keys, Terraform state, `.env` files, project-specific secrets, or production config. Keep Cloud Run deployments private and preserve safety guardrails for billing-disable behavior.
