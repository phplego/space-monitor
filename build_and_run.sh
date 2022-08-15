rm ./space-monitor

go build

if [[ $? -eq 0 ]]
then
    ./space-monitor
fi

