go build

if [[ $? -eq 0 ]]
then
    ./space-monitor --nosave
fi

