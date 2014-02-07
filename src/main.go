package main

import (
    "container/list"
    "container/ring"
    "fmt"
    "net/http"
    "encoding/json"
    "os"
    "strconv"
    "time"
    "errors"
)

var (
    room *Room
)

type User struct {
    Username string
    LastPollTime *time.Time
    c chan *ChatMessage
    quit chan bool
}

type ChatMessage struct {
    Username string
    Body string
    TimeStamp *time.Time
}

type Room struct {
    Users *list.List
    Messages *ring.Ring
    c chan *ChatMessage
}

func NewRoom() *Room {
    r := new(Room)
    r.Users = list.New()
    r.Messages = ring.New(20)
    return r
}

func NewUser(username string) *User {
    u := &User{Username: username}
    u.c = make(chan *ChatMessage, 20)
    return u
}

func (r *Room)getUserElement(username string) (*list.Element, *User) {
    for e := r.Users.Front(); e != nil; e = e.Next() {
        user := e.Value.(*User)
        if user.Username == username {
            return e, user
        }
    }
    return nil, nil
}

func (r *Room)AddUser(username string) (*User, error) {
    user := r.GetUser(username)
    if user != nil {
        return nil, errors.New("That username is already taken.")
    }
    user = NewUser(username)
    r.Users.PushBack(user)
    fmt.Printf("\tUser %s has entered the room.\n", user.Username)
    return user, nil
}

func (r *Room)RemoveUser(username string) bool {
    if e, _ := r.getUserElement(username); e != nil {
        r.Users.Remove(e)
        fmt.Printf("\tUser %s has left the room.\n", username)
        return true
    }
    return false
}

func (r *Room)GetUser(username string) *User {
    _, user := r.getUserElement(username)
    return user
}

func (r *Room)AddMessage(msg *ChatMessage) {
    r.Messages = r.Messages.Next()
    r.Messages.Value = msg
    for e := r.Users.Front(); e != nil; e = e.Next() {
        user := e.Value.(*User)
        user.c <- msg
    }
}

func (m *ChatMessage)WriteToResponse(w http.ResponseWriter) {
    w.Header()["Content-Type"] = []string{"application/json"}
    raw, err := json.Marshal(m)
    if err != nil {
        fmt.Fprintf(os.Stderr, "something got fucked up in json.Marshal.\n")
    } else {
        w.Write(raw)
    }
}

func ParseJSONField(r *http.Request, fieldname string) (string, error) {
    requestLength, err := strconv.ParseUint(r.Header["Content-Length"][0], 10, 32)
    if err != nil {
        fmt.Fprintf(os.Stderr, "unable to convert incoming login request content-lenth to uint.")
    }
    var l map [string] string
    raw := make([]byte, requestLength)
    r.Body.Read(raw)
    if err := json.Unmarshal(raw, &l); err != nil {
        return "", err
    }
    return l[fieldname], nil
}

// given an http.Request r, returns the username associated with the given
// request, as determined with an extremely unsafe cookie.  Returns an empty
// string if the user is not logged in.
func ParseUsername(r *http.Request) string {
    for _, c := range r.Cookies() {
        if c.Name == "username" {
            return c.Value
        }
    }
    return ""
}

func ParseMessage(r *http.Request) (*ChatMessage, error) {
    msgLength, err := strconv.ParseUint(r.Header["Content-Length"][0], 10, 32)
    if err != nil {
        fmt.Fprintf(os.Stderr, "unable to convert incoming message content-length to uint.")
    }
    from := room.GetUser(ParseUsername(r))

    fmt.Printf("\tReceived message from user %s with length %d\n", from.Username, msgLength)
    t := time.Now().UTC()
    m := &ChatMessage{Username: from.Username, TimeStamp: &t}
    raw := make([]byte, msgLength)
    r.Body.Read(raw)
    if err := json.Unmarshal(raw, m); err != nil {
        fmt.Fprintf(os.Stderr, "%s\n", err)
    }
    return m, err
}

func Home(w http.ResponseWriter, r *http.Request) {
    if r.URL.Path == "/favicon.ico" {
        return
    }
    fmt.Fprintf(os.Stdout, "%s %s\n", r.Method, r.URL.Path)
    http.ServeFile(w, r, "templates/index.html")
}

func LoginMux(w http.ResponseWriter, r *http.Request) {
    fmt.Fprintf(os.Stdout, "%s %s\n", r.Method, r.URL.Path)
    switch r.Method {
    case "POST":
        Login(w, r)
    case "DELETE":
        Logout(w, r)
    }
}

func Login(w http.ResponseWriter, r *http.Request) {
    username, err := ParseJSONField(r, "username")
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    user, err := room.AddUser(username)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    cookie := &http.Cookie{Name: "username", Value: user.Username, HttpOnly: true}
    http.SetCookie(w, cookie)
}

func Logout(w http.ResponseWriter, r *http.Request) {
    username := ParseUsername(r)
    if username == "" {
        http.Error(w, "That username wasn't actually logged in.", http.StatusInternalServerError)
    }
    room.RemoveUser(username)
}

func FeedMux(w http.ResponseWriter, r *http.Request) {
    fmt.Fprintf(os.Stdout, "%s %s\n", r.Method, r.URL.Path)
    switch r.Method {
    case "GET":
        Poll(w, r)
    case "POST":
        Post(w, r)
    }
}

func Post(w http.ResponseWriter, r *http.Request) {
    m, err := ParseMessage(r)
    if err != nil {
        http.Error(w, "Unable to parse incoming chat message", http.StatusInternalServerError)
    }
    room.AddMessage(m)
    fmt.Fprintf(os.Stdout, "\t%s: %s\n", m.Username, m.Body)
}

func Poll(w http.ResponseWriter, r *http.Request) {
    timeout := make(chan bool)
    go func() {
        time.Sleep(1.2e11) // two minutes.
        timeout <- true
    }()
    user := room.GetUser(ParseUsername(r))

    if user == nil {
      msg := fmt.Sprintf("Cannot find user %s\n", ParseUsername(r))

      http.Error(w, msg, http.StatusInternalServerError)
      fmt.Fprintf(os.Stderr, msg)
      return
    }

    var msg *ChatMessage

    if user.c != nil {
        select {
        case msg = <-user.c:
            msg.WriteToResponse(w)
        case <-timeout: return
        }
    } else {
        fmt.Fprintf(os.Stderr, "the user %s has a null incoming channel.\n", user.Username)
        return
    }
}

func main() {
    port := "0.0.0.0:8080"
    room = NewRoom()
    staticServer := http.FileServer(http.Dir("./static"))

    http.HandleFunc("/", Home)
    http.HandleFunc("/feed", FeedMux)
    http.HandleFunc("/login", LoginMux)
    http.Handle("/static/", http.StripPrefix("/static/", staticServer))
    fmt.Printf("Serving at %s ----------------------------------------------------\n", port)
    http.ListenAndServe(port, nil)
}
