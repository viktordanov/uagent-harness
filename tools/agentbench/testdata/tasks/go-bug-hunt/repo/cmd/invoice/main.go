// Command invoice prints a sample invoice.
package main

import (
	"fmt"

	"example.com/invoice/invoice"
)

func main() {
	cat := invoice.Catalog{"MUG": 1250, "TEE": 1999}
	l1, _ := cat.Line("MUG", 1)
	l2, _ := cat.Line("TEE", 2)
	fmt.Print(invoice.Format(invoice.Price(invoice.Order{Lines: []invoice.Line{l1, l2}, Code: "SPRING15", Region: "CA"})))
}
