// Author: Larshavin
// Date: 2023/10/30
// Func: main function
// This is a Web Server for Korean Chess Game that called Jang-gi
// It use Fiber Framework and PostgreSQL Database
// And It use Vue.js for Frontend Framework and PrimeVue for UI Framework
// Websocket is used for real-time communication between server and client

/*
장기의 게임 서버를 구축하기 위해서는,
1.	매치메이킹을 담당하는 기능을 개발해야 한다.
	프론트엔드에서 각각의 유저 (시작은 웹소켓 연결시 가상의 유저를 생성)가 매치메이킹을 요청하면 대기열에서 상대를 기다리게 된다.
	상대가 매칭되면, 둘 만의 방을 생성하고, 각각의 유저에게 방의 정보를 전달한다. (방의 정보는 방의 고유 ID, 방의 이름, 방의 비밀번호, 방의 상태 등이다.)
	이후, 각각의 유저는 방의 정보를 통해 서로의 상태를 확인하고, 게임을 시작할 수 있다.
2. 	게임 시작 전에 각 유저는 진영을 선택해야 한다. (랜덤)
	또한 각 유저는 게임 시작 전에, 자신의 진영에 맞는 말을 배치해야 한다. (마상상마, 마상마상, 상마상마, 상마마상)
	장기판의 정보는 초나라가 아래 있다는 기준이며, 한나라 진영으로 정보를 보낼 때는 배열의 반대로 보내지게 된다.
	ex) 행 : 0 ~ 9, 열 : 0 ~ 8
		만약 초나라의 차가 (9,0)에 있다가 (8,0)로 움직인다면, 한나라 유저에게는 (0,8)에서 (1,8)으로 움직인 것으로 보여진다.
		즉, 행렬의 대각 대칭과 같은 형태로 보내지게 된다.
3. 	기물의 움직임과 장군의 움직임은 각각의 프론트엔드에서 계산되게 된다. 따라서 게임 서버에서는 단순히 기물 이동 정보와 게임 종료 정보만을 전달하게 된다.
	만약 게임이 종료 된다면, 그 게임 정보는 각각의 유저 정보와 함께 데이터베이스에 저장되게 된다.
*/

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
)

func main() {
	app := fiber.New()

	app.Use("/ws", func(c *fiber.Ctx) error {
		// IsWebSocketUpgrade returns true if the client
		// requested upgrade to the WebSocket protocol.
		if websocket.IsWebSocketUpgrade(c) {
			c.Locals("allowed", true)
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})

	app.Get("/ws/matchMaking:id", matchMaking())

	log.Fatal(app.Listen(":3000"))
	// Access the websocket server: ws://localhost:3000/ws/123?v=1.0
	// https://www.websocket.org/echo.html
}

type User struct {
	conn   *websocket.Conn
	ID     string
	Rating int
	side   int // 8 : 초나라, 16 : 한나라
}

type Room struct {
	ID   string
	Name string
	User [2]User
	Time time.Time
}

type Game struct {
	Room  Room
	User  [2]User
	Move  string
	Turn  int
	Over  bool
	Start time.Time
}

// 매치 메이킹 웹소켓 핸들러
func matchMaking() func(*fiber.Ctx) error {
	return websocket.New(func(c *websocket.Conn) {
		// c.Locals is added to the *websocket.Conn
		log.Println(c.Locals("allowed")) // true
		log.Println(c.Params("id"))      // 123
		// log.Println(c.Cookies("session")) // ""

		// websocket.Conn bindings https://pkg.go.dev/github.com/fasthttp/websocket?tab=doc#pkg-index
		var (
			mt  int
			msg []byte
			err error
		)
		for {
			if mt, msg, err = c.ReadMessage(); err != nil {
				log.Println("read:", err)
				break
			}
			fmt.Println(mt, string(msg))

			var user User
			// msg 변수를 User 구조체로 변환
			json.Unmarshal(msg, &user)
			user.conn = c
			users[user.ID] = &user

			// 추후 추가 : db에서 유저 정보를 끌어와서 레이팅을 설정한다.

			// 대기열 채널에 User.ID를 추가 후 매칭을 기다림
			//	  채널에서 매칭이 되면, 두 유저의 정보를 받아서 방 구조체를 생성 후 맵에 추가
			//    생성된 방 구조체의 정보 (접속 경로, 유저 정보, 진영 정보)를 각각의 유저에게 전달
			matchMakingRequest(user)

			//
		}
	})
}

// 매치메이킹 채널
var (
	matchQueue = make(chan *User)
	users      = make(map[string]*User)
)

// 고루틴 함수 : 매치메이킹 프로세스
func matchMakingRequest(user User) {
	// matchMakingChannel = make(chan User)

	// 채널에 유저가 들어오면, 대기열 슬라이스에 유저 추가
	matchQueue <- &user
}

func matchUsers() {
	for {
		select {
		case user := <-matchQueue:
			// Try to find a match for the client
			for _, otherUser := range users {
				if user.ID != otherUser.ID {
					// Found a match, handle the matchmaking logic here
					MatchUsersLogic(user, otherUser)
				}
			}
		}
	}
}

func MatchUsersLogic(user1, user2 *User) {
	// Implement your matchmaking logic here
	// 추후 추가 : 레이팅을 비교해서 매칭을 시도한다.

	// For example, you can send a message to both clients that they are matched
	user1.conn.WriteMessage(websocket.TextMessage, []byte("You are matched!"))
	user2.conn.WriteMessage(websocket.TextMessage, []byte("You are matched!"))

	// Remove clients from the matchmaking queue and clients map
	delete(users, user1.ID)
	delete(users, user2.ID)

	// 웹소켓 종료
	user1.conn.Close()
	user2.conn.Close()
}
