


/*
Server (You)                     Client (User)
      +-------------+                 +-------------+
      |             |                 |             |
      |  ReadPump   | <---(Lane 1)--- |   Browser   |  (User types "Hello")
      |   (Ear)     |                 |             |
      |             |                 |             |
      |  WritePump  | ---(Lane 2)---> |   Browser   |  (Server sends "Welcome")
      |   (Mouth)   |                 |             |
      +-------------+                 +-------------+


*/


User Connects: ws://localhost:8080/ws?token=...

    Connection established. User is in NO rooms.
 

User Chats:

    Sends: {"type": "message", "content": "Goal!", "room": "sports"}

    Server: Only people in the "sports" room see this.

 Here is the checklist to fix this:

    User B must send {"type": "join", "content": "sports"} first.

    User A then sends {"type": "message", "room": "sports", "content": "Goal!"}.

    The Server routes the message only to people in the "sports" list.

