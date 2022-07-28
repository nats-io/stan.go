#!/bin/bash -e
# Run from directory above via ./scripts/cov.sh

go install github.com/mattn/goveralls@latest
go install github.com/wadey/gocovmerge@latest

rm -rf ./cov
mkdir cov
go test -v -covermode=atomic -coverprofile=./cov/stan.out -modfile go_tests.mod
gocovmerge ./cov/*.out > acc.out
rm -rf ./cov

# If we have an arg, assume travis run and push to coveralls. Otherwise launch browser results
if [[ -n $1 ]]; then
    $HOME/gopath/bin/goveralls -coverprofile=acc.out
    rm -rf ./acc.out
else
    go tool cover -html=acc.out
fi
