package main

import (
    "container/list"
    "container/ring"
    "fmt"
    "http"
    "json"
    "os"
    "strconv"
    "time"
)

var (
    room *Room
)

type User struct {
    Username string
    LastPollTime *time.Time
    c chan *ChatMessage
    quit chan bool
    w http.ResponseWriter
}

type ChatMessage struct {
    User *User
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
    r.c = make(chan *ChatMessage)
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

func (r *Room)AddUser(username string) (*User, os.Error) {
    user := r.GetUser(username)
    if user != nil {
        return nil, os.NewError("That username is already taken.")
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

func ParseJSONField(r *http.Request, fieldname string) (string, os.Error) {
    requestLength, err := strconv.Atoui(r.Header["Content-Length"][0])
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

func ParseMessage(r *http.Request) (*ChatMessage, os.Error) {
    msgLength, err := strconv.Atoui(r.Header["Content-Length"][0])
    if err != nil {
        fmt.Fprintf(os.Stderr, "unable to convert incoming message content-length to uint.")
    }
    from := room.GetUser(ParseUsername(r))

    m := &ChatMessage{User: from, TimeStamp: time.UTC()}
    raw := make([]byte, msgLength)
    r.Body.Read(raw)
    if err := json.Unmarshal(raw, m); err != nil {
        fmt.Fprintf(os.Stderr, "%s\n", err)
    }
    return m, err
}

func Home(w http.ResponseWriter, r *http.Request) {
    if r.RawURL == "/favicon.ico" {
        return
    }
    fmt.Fprintf(os.Stdout, "%s %s\n", r.Method, r.RawURL)
    http.ServeFile(w, r, "templates/index.html")
}

func LoginMux(w http.ResponseWriter, r *http.Request) {
    fmt.Fprintf(os.Stdout, "%s %s\n", r.Method, r.RawURL)
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
        http.Error(w, err.String(), http.StatusInternalServerError)
        return
    }

    user, err := room.AddUser(username)
    if err != nil {
        http.Error(w, err.String(), http.StatusInternalServerError)
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
    fmt.Fprintf(os.Stdout, "%s %s\n", r.Method, r.RawURL)
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
    fmt.Fprintf(os.Stdout, "\t%s: %s\n", m.User.Username, m.Body)
}

func Poll(w http.ResponseWriter, r *http.Request) {
    user := room.GetUser(ParseUsername(r))
    var msg *ChatMessage
    if user.c != nil {
        msg = <-user.c
        fmt.Fprintf(os.Stderr, "the user %s has a null incoming channel.\n", user.Username)
    }
    w.Header()["Content-Type"] = []string{"application/json"}
    raw, err := json.Marshal(msg)
    if err != nil {
        fmt.Fprintf(os.Stderr, "something got fucked up in json.Marshal.\n")
    } else {
        w.Write(raw)
    }
}

func main() {
    room = NewRoom()
    staticDir := http.Dir("/projects/go/chat/static")
    staticServer := http.FileServer(staticDir)

    http.HandleFunc("/", Home)
    http.HandleFunc("/feed", FeedMux)
    http.HandleFunc("/login", LoginMux)
    http.Handle("/static/", http.StripPrefix("/static", staticServer))
    fmt.Println("Serving at localhost:8080 ----------------------------------------------------")
    http.ListenAndServe(":8080", nil)
}
