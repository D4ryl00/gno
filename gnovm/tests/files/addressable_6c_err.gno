package main

func main() {
	var i any
	i = [1]int{1}
	println(&i.([1]int)[0])
}

// Error:
// main/files/addressable_6c_err.gno:6:10: cannot take address of i<VPBlock(1,0)>.([(const (1 int))](const-type int))[(const (0 int))]

// Output:
// &(1 int)

// TypeCheckError:
// main/files/addressable_6c_err.gno:6:11: invalid operation: cannot take address of i.([1]int)[0] (value of type int)
