package main

import(
	"io/ioutil"
	"encoding/json"
	//"os"
	"log"
	"regexp"
	"net/http"
	"html/template"
	"path/filepath"

	"github.com/gorilla/sessions"
	"github.com/google/uuid"
	"github.com/shomali11/util/xhashes"

	"github.com/serg666/accountstorage/chaincode"

	"github.com/hyperledger/fabric-sdk-go/pkg/core/config"
	"github.com/hyperledger/fabric-sdk-go/pkg/gateway"
)

type Page struct {
	Title   string
	Account chaincode.Account
	Chunks  []interface{}
}

var (
	key = []byte("super-secret-key")
	templates = template.Must(template.ParseFiles("main.html", "login.html", "account.html", "history.html", "transfer.html"))
	store = sessions.NewCookieStore(key)
	validPath = regexp.MustCompile("^/(history|transfer)/([a-zA-Z0-9-]+)$")
	wallet *gateway.Wallet
	gw *gateway.Gateway
	network *gateway.Network
	contract *gateway.Contract
	err error
)

func renderTemplate(w http.ResponseWriter, tmpl string, p *Page) {
	err = templates.ExecuteTemplate(w, tmpl+".html", p)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func historyHandler(w http.ResponseWriter, r *http.Request, account string) {
	session, _ := store.Get(r, "cookie-name")
	if auth, ok := session.Values["authenticated"].(bool); !ok || !auth {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	email, _ := session.Values["email"].(string)
	result, err := contract.EvaluateTransaction("ReadAccount", account)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var acc chaincode.Account
	err = json.Unmarshal(result, &acc)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	result, err = contract.EvaluateTransaction("GetAccountHistory", account)
	p := &Page{
		Title: email,
		Account: acc,
	}
	if err == nil {
		_ = json.Unmarshal(result, &p.Chunks)
	}
	renderTemplate(w, "history", p)
}

func transferHandler(w http.ResponseWriter, r *http.Request, account string) {
	session, _ := store.Get(r, "cookie-name")
	if auth, ok := session.Values["authenticated"].(bool); !ok || !auth {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	email, _ := session.Values["email"].(string)
	result, err := contract.EvaluateTransaction("ReadAccount", account)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var sender chaincode.Account
	err = json.Unmarshal(result, &sender)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case "GET":
		result, err = contract.EvaluateTransaction("GetAllParticipants")
		p := &Page{
			Title: email,
			Account: sender,
		}
		if err == nil {
			_ = json.Unmarshal(result, &p.Chunks)
		}
		renderTemplate(w, "transfer", p)
	case "POST":
		if err := r.ParseForm(); err != nil {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}

		result, err := contract.EvaluateTransaction("GetParticipantAccounts", r.FormValue("recipient"))
		if err != nil {
			log.Printf("Can not get participant accounts %s: %v\n", r.FormValue("recipient"), err)
			http.NotFound(w, r)
			return
		}

		var accounts []chaincode.Account
		err = json.Unmarshal(result, &accounts)
		if err != nil {
			log.Printf("Failed to unmarshal participant accounts %s: %v\n", r.FormValue("recipient"), err)
			http.NotFound(w, r)
			return
		}

		for _, recipient := range accounts {
			if recipient.Currency == sender.Currency {
				_, err := contract.SubmitTransaction("Transaction", sender.ID, recipient.ID, r.FormValue("amount"))
				if err != nil {
					log.Printf("Failed to transfer units from %s to %s of amount %v: %v\n", sender.ID, recipient.ID, r.FormValue("amount"), err)
					http.NotFound(w, r)
					return
				}

				http.Redirect(w, r, "/", http.StatusFound)
				return
			}
		}

		log.Printf("Can not find appropriate account for recipient %s\n", r.FormValue("recipient"))
		http.NotFound(w, r)
	default:
		http.Redirect(w, r, "/", http.StatusFound)
	}
}

func makeHandler(fn func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m := validPath.FindStringSubmatch(r.URL.Path)
		if m == nil {
			http.NotFound(w, r)
			return
		}
		fn(w, r, m[2])
	}
}

func login_page(w http.ResponseWriter, r *http.Request) {
	renderTemplate(w, "login", &Page{Title: "Login"})
}

func logout_page(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "cookie-name")
	// Revoke users authentication
	session.Values["authenticated"] = false
	session.Save(r, w)
	http.Redirect(w, r, "/", http.StatusFound)
}

func auth_page(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "cookie-name")
	// Authentication goes here
	if r.Method == "POST" {
		err = r.ParseForm()
		if err == nil {
			result, err := contract.EvaluateTransaction("ParticipantExists", r.FormValue("email"))
			log.Printf("Participant exists: %s %v\n", string(result), err)
			if string(result) == "false" {
				_, err = contract.SubmitTransaction("CreateParticipant", r.FormValue("email"), "name", "surname", "+79999999999", r.FormValue("password"))
				log.Printf("Create participant: %v\n", err)
				session.Values["authenticated"] = err == nil
				session.Values["email"] = r.FormValue("email")
			} else {
				result, err := contract.EvaluateTransaction("ReadParticipant", r.FormValue("email"))
				log.Printf("Read participant: %v\n", err)
				if err == nil {
					var participant chaincode.Participant
					err = json.Unmarshal(result, &participant)
					session.Values["authenticated"] = err == nil && participant.Passwd == xhashes.MD5(r.FormValue("password"))
					session.Values["email"] = r.FormValue("email")
				}
			}
		}
	}
	session.Save(r, w)
	http.Redirect(w, r, "/", http.StatusFound)
}

func account_page(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "cookie-name")
	if auth, ok := session.Values["authenticated"].(bool); !ok || !auth {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	email, _ := session.Values["email"].(string)
	switch r.Method {
	case "GET":
		p := &Page{
			Title: email,
		}
		renderTemplate(w, "account", p)
	case "POST":
		if err := r.ParseForm(); err != nil {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}

		_, _ = contract.SubmitTransaction("CreateAccount", uuid.New().String(), r.FormValue("currency"), r.FormValue("balance"), email)
		http.Redirect(w, r, "/", http.StatusFound)
	default:
		http.Redirect(w, r, "/", http.StatusFound)
	}
}

func main_page(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "cookie-name")
	if auth, ok := session.Values["authenticated"].(bool); !ok || !auth {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	email, _ := session.Values["email"].(string)
	result, err := contract.EvaluateTransaction("GetParticipantAccounts", email)
	p := &Page{
		Title: email,
	}
	if err == nil {
		_ = json.Unmarshal(result, &p.Chunks)
	}
	renderTemplate(w, "main", p)
}

func populateWallet(wallet *gateway.Wallet) error {
	log.Println("============ Populating wallet ============")
	cert, err := ioutil.ReadFile(filepath.Clean("User1@org1.example.com-cert.pem"))
	if err != nil {
		return err
	}

	key, err := ioutil.ReadFile(filepath.Clean("priv_sk"))
	if err != nil {
		return err
	}

	identity := gateway.NewX509Identity("Org1MSP", string(cert), string(key))

	return wallet.Put("appUser", identity)
}

func main () {
	log.SetPrefix("accstorageapp: ")
	log.Println("============ application starts ============")
	//err = os.Setenv("DISCOVERY_AS_LOCALHOST", "true")
	//if err != nil {
	//	log.Fatalf("Error setting DISCOVERY_AS_LOCALHOST environemnt variable: %v", err)
	//}

	wallet, err = gateway.NewFileSystemWallet("wallet")
	if err != nil {
		log.Fatalf("Failed to create wallet: %v", err)
	}

	if !wallet.Exists("appUser") {
		err = populateWallet(wallet)
		if err != nil {
			log.Fatalf("Failed to populate wallet contents: %v", err)
		}
	}

	gw, err = gateway.Connect(
		gateway.WithConfig(config.FromFile(filepath.Clean("connection-org1.yaml"))),
		gateway.WithIdentity(wallet, "appUser"),
	)
	if err != nil {
		log.Fatalf("Failed to connect to gateway: %v", err)
	}
	defer gw.Close()

	network, err = gw.GetNetwork("test1")
	if err != nil {
		log.Fatalf("Failed to get network: %v", err)
	}

	contract = network.GetContract("test1")

	http.HandleFunc("/", main_page)
	http.HandleFunc("/login", login_page)
	http.HandleFunc("/logout", logout_page)
	http.HandleFunc("/account", account_page)
	http.HandleFunc("/auth", auth_page)
	http.HandleFunc("/history/", makeHandler(historyHandler))
	http.HandleFunc("/transfer/", makeHandler(transferHandler))
	log.Fatal(http.ListenAndServe("192.168.0.107:9090", nil))
}
