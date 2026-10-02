# accounts

A small service that stores users. The central type is `model.Account`,
built with `model.NewAccount`; the store keeps Account values by ID,
the service validates them, and the API renders them as JSON.
