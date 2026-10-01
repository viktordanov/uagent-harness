// Command server registers two users and prints them.
package main

import (
	"fmt"

	"example.com/accounts/api"
	"example.com/accounts/model"
	"example.com/accounts/service"
	"example.com/accounts/store"
)

func main() {
	st := store.New()
	svc := service.New(st)
	_, _ = svc.Register("ann@example.com", "Ann")
	_, _ = svc.Register("bob@example.com", "Bob")
	var users []model.Account = st.List()
	b, _ := api.EncodeList(users)
	fmt.Println(string(b))
}
