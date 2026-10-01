# accounts

A small service that stores users. The central type is `model.UserRecord`,
built with `model.NewUserRecord`; the store keeps UserRecord values by ID,
the service validates them, and the API renders them as JSON.
