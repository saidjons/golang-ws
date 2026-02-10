


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
