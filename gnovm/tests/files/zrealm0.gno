// PKGPATH: gno.land/r/test
package test

var root any

func main() {
	println(root)
	root = 1
	println(root)
}

// Output:
// nil
// 1

// The below tests that the realm's block (of 1 variable) changed.  The first
// element image in the package (block) is for the "main" function, which
// appears first because function declarations are defined in a file before
// vars.

// Realm:
// switchrealm["gno.land/r/test"]
// u[a8ada09dee16d791fd406d629fe29bb0ed084a30:2]=
//     @@ -19,11 +19,10 @@
//          },
//          "Values": [
//              {
//     +            "N": "AQAAAAAAAAA=",
//                  "T": {
//     -                "@type": "/gno.InterfaceType",
//     -                "Generic": "",
//     -                "Methods": null,
//     -                "PkgPath": ""
//     +                "@type": "/gno.PrimitiveType",
//     +                "value": "32"
//                  }
//              },
//              {
