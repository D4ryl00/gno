package main

func main() {
	greeting := "hello"
	_ = &greeting[2]
}

// Error:
// main/files/addressable_4a_err.gno:5:6: cannot take address of greeting<VPBlock(1,0)>[(const (2 int))]

// TypeCheckError:
// main/files/addressable_4a_err.gno:5:7: invalid operation: cannot take address of greeting[2] (value of type byte)
