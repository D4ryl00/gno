package main

func main() {
	x := 11
	y := -(1 << x)
	println(y)
}

// Output:
// -2048
