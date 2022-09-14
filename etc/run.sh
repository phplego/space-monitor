#!/usr/bin/env bash

gum style \
  --border-foreground 112 --border double \
   --margin "1 2" --padding "1 4" \
  "EXECUTE GO FILE"

echo "Choose file to execute:"
echo 
SELECTED=$(ls *.go | gum choose) || exit
while read file ; do
  echo "execute file $file ..."
  go run $file
done <<< $SELECTED


read -p "Press any key to exit..."

