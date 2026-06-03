# 1.  create a room
curl -X POST localhost:8080/rooms -d '{"id":"cats","name":"Cat pics","description":"meow"}'

# 2.  grab a JWT for alice
TOKEN=$(curl -s localhost:8080/login -d '{"user":"saidjon","password":"123"}' | jq -r .token)

# 3.  authorise alice to join room cats
curl -X POST localhost:8080/rooms/cats/members -d '{"user_id":"saidjon"}'

# 4.  connect with websocat for alice
websocat "ws://localhost:8080/rooms/cats/ws?token=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VyX2lkIjoiYWxpY2UiLCJjaGFubmVscyI6WyJwdWJsaWMiLCJyb29tLWFsaWNlIl19.dt9OCBePXkoYBW80HT4kB7jTKVnT5tuLymb6hT6R3sQ"
# 4.  connect with websocat for saidjon
websocat "ws://localhost:8080/rooms/cats/ws?token=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VyX2lkIjoic2FpZGpvbiIsImNoYW5uZWxzIjpbInB1YmxpYyIsInJvb20tc2FpZGpvbiJdfQ.XESMdIxqFM3F6lVOgk6llBvvdRBHpepNqnIDkBmaRDo"

# 5.  in another terminal do the same for bob (after adding bob to members)
# every message either user types is delivered only to authorised members of room cats.