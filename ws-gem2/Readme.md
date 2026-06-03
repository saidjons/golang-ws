


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

    join room  {"type": "join", "content": "sports"} first.

    send message {"type": "message", "room": "sports", "content": "Goal!"}.
    
    get online users {"type": "get_users", "content": "general"}

    

 

