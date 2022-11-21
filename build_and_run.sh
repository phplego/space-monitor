#go build -ldflags "-X 'main.Version=0.0.1' -X 'main.BuildTime=$(date)'"
go build

if [[ $? -eq 0 ]]
then
    ./space-monitor $@
fi

