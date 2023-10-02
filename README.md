Golang boilerplate application

### Project Configuration

```bash
git clone git@github.com:arf-labs-ou/go-boilerplate.git `{new-repo-name}`

rm -rf .git

go mod edit -module {new-repository-path}
```

Replace all `go-boilerplate` texts with `{new-repo-name}`in your project.

For sonarcloud edit your projectkey from sonar-project.properties file.


```bash
git init

git remote add origin {repo}

git branch -M master
```



### Development Installation

```
brew install pre-commit
brew install golangci-lint
go install github.com/fzipp/gocyclo/cmd/gocyclo@latest
brew install goreleaser
go install github.com/golang/mock/mockgen@v1.6.0
go install github.com/swaggo/swag/cmd/swag@latest
go install golang.org/x/tools/cmd/goimports@latest
```

```
pre-commit install
```