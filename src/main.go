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
    rooms map[string] *Room
    room chan *ChatMessage
    users *list.List
    messageHistory *ring.Ring
)

type User struct {
    Username string
    LastPollTime *time.Time
    ResponseChan chan *ChatMessage
}

type ChatMessage struct {
    Username string
    Body string
    TimeStamp *time.Time
}

type Room struct {
    Title string
    Users *list.List
    Messages *ring.Ring
    c chan *ChatMessage
}

func ParseJSONField(r *http.Request, fieldname string) (username string, err os.Error ) {
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

func ParseUser(r *http.Request) *User {
    username := ParseUsername(r)
    if username == "" {
        return nil
    }
    for node := users.Front(); node != nil; node = node.Next() {
        user := node.Value.(*User)
        if user.Username == username {
            return user
        }
    }
    return nil
}

func ParseMessage(r *http.Request) (*ChatMessage, os.Error) {
    msgLength, err := strconv.Atoui(r.Header["Content-Length"][0])
    if err != nil {
        fmt.Fprintf(os.Stderr, "unable to convert incoming message content-length to uint.")
    }
    m := &ChatMessage{Username: ParseUsername(r), TimeStamp: time.UTC()}
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
    }
    for node := users.Front(); node != nil; node = node.Next() {
        user := node.Value.(*User)
        if user.Username == username {
            http.Error(w, "That username is already taken.", http.StatusForbidden)
            return
        }
    }
    users.PushBack(&User{Username: username})
    c := &http.Cookie{Name: "username", Value: username, HttpOnly: true}
    http.SetCookie(w, c)
}

func Logout(w http.ResponseWriter, r *http.Request) {
    fmt.Println(os.Stdout, "inside logout")
    username, err := ParseJSONField(r, "username")
    if err != nil {
        http.Error(w, err.String(), http.StatusInternalServerError)
    }
    for e := users.Front(); e != nil; e = e.Next() {
        if username == e.Value {
            users.Remove(e)
            break
        }
    }
}

func FeedMux(w http.ResponseWriter, r *http.Request) {
    switch r.Method {
    case "GET":
        fmt.Fprintf(os.Stdout, "%s %s\n", r.Method, r.RawURL)
        Poll(w, r)
    case "POST":
        fmt.Fprintf(os.Stdout, "%s %s \n", r.Method, r.RawURL)
        PostMsg(w, r)
    }
}

func PostMsg(w http.ResponseWriter, r *http.Request) {
    m, err := ParseMessage(r)
    if err != nil {
        http.Error(w, "Unable to parse incoming chat message", http.StatusInternalServerError)
    }
    messageHistory = messageHistory.Next()
    messageHistory.Value = m
    fmt.Fprintf(os.Stdout, "%s: %s\n", m.Username, m.Body)
    fmt.Fprintf(os.Stdout, "%s\n", messageHistory)
}

func Poll(w http.ResponseWriter, r *http.Request) {
    user := ParseUser(r)
    w.Header()["Content-Type"] = []string{"application/json"}
    if user.LastPollTime == nil {
        first := true
        w.Write([]byte("["))
        messageHistory.Do(func(item interface{}) {
            if item == nil {
                return
            }
            if raw, err := json.Marshal(item); err == nil {
                if !first {
                    w.Write([]byte(","))
                } else {
                    first = false
                }
                w.Write(raw)
            } else {
                fmt.Fprintf(os.Stderr, "Poll error: %s\n", err)
            }
        })
        w.Write([]byte("]"))
    }
}

func main() {
    users = list.New()
    messageHistory = ring.New(20)
    staticDir := http.Dir("/projects/go/chat/static")
    staticServer := http.FileServer(staticDir)

    http.HandleFunc("/", Home)
    http.HandleFunc("/feed", FeedMux)
    http.HandleFunc("/login", LoginMux)
    http.Handle("/static/", http.StripPrefix("/static", staticServer))
    http.ListenAndServe(":8080", nil)
}
