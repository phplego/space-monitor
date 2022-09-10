#!/usr/bin/env bash

# COMMIT
# ------
if [[ `git status --porcelain` ]]; then
  # Changes
  LIST=$(git status --porcelain)

  gum style \
  	--border-foreground 212 --border double \
  	 --margin "1 2" --padding "1 4" \
  	"CHANGES:" "" "$LIST"

  gum confirm "Add these files?" || exit

  git add . # ADD EVERYTHING!

  DESC=$(gum input --placeholder "Details of this change [ENTER to finish]") || exit

  gum confirm "Commit changes with message '$DESC'? " || exit
  git commit -m "$DESC"

else
  # No changes
  echo "No local changes."
fi

# PUSH
# ----
if [[ `git diff --stat @{upstream}` ]]; then
  # Unpushed commits
  git diff --stat @{upstream}
  gum confirm "Unpushed commits found. Do you want to push them?" || exit

  git push || echo "Push failed!" && exit
  echo "Pushed successfully"
else
  # No Unpushed commits
  echo "No diff with remote. Everything up to date. Exit"
fi


