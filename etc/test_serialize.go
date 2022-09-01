package main

import (
    "encoding/gob"
    "fmt"
    "os"
)

type Badge struct {
    Gold, Silver, Bronze int
}

type Account struct {
    User   string
    Active bool
    Badges Badge
}

func main() {
    //////////
    // First lets encode some data
    //////////

    // Dummy data
    accounts := make(map[string]Account)
    accounts["User1"] = Account{"User1", true, Badge{4, 0, 2}}
    accounts["User2"] = Account{"UserTwo", false, Badge{100, 7, 12}}

    // Create a file for IO
    encodeFile, err := os.Create("test_serialize.gob")
    if err != nil {
	panic(err)
    }

    // Since this is a binary format large parts of it will be unreadable
    encoder := gob.NewEncoder(encodeFile)

    // Write to the file
    if err := encoder.Encode(accounts); err != nil {
	panic(err)
    }
    encodeFile.Close()

    //////////
    // Now let's decode that data
    //////////

    // Open a RO file
    decodeFile, err := os.Open("test_serialize.gob")
    if err != nil {
	panic(err)
    }
    defer decodeFile.Close()

    // Create a decoder
    decoder := gob.NewDecoder(decodeFile)

    // Place to decode into
    accounts2 := make(map[string]Account)

    // Decode -- We need to pass a pointer otherwise accounts2 isn't modified
    decoder.Decode(&accounts2)

    // And let's just make sure it all worked
    fmt.Println("Accounts1:", accounts)
    fmt.Println("Accounts2:", accounts2)
}