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
	"sync"
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

	app.Get("/ws/match", matchMaking())

	go matchUsers()

	log.Fatal(app.Listen(":3000"))
	// Access the websocket server: ws://localhost:3000/ws/123?v=1.0
	// https://www.websocket.org/echo.html
}

type User struct {
	conn   *websocket.Conn
	ID     string `json:"id"`
	Rating int
	side   int   // 8 : 초나라, 16 : 한나라
	room   *Room // 방 정보를 저장하는 포인터
}

type Room struct {
	ID     string
	Users  [2]*User
	Status int // 0 : 대기, 1 : 게임 중, 2 : 게임 종료
}
type Game struct {
	// Room  Room
	// User  [2]User
	Move [2]int    `json:"move"`
	Turn int       `json:"turn"`
	Over bool      `json:"over"`
	Time time.Time `json:"time"`
}

// 웹소켓 통신에 사용되는 메세지는 무조건 아래 구조체를 따른다.
// 메시지 구조체를 직렬화 하여 행동과 메세지로 구별된 json을 클라이언트가 받는다.
type Message struct {
	Action string `json:"action"`
	Msg    []byte `json:"msg"`
}

// var 정의
var (
	rwMutex    = new(sync.RWMutex)
	matchQueue = make(chan *User, 1)
	users      = make(map[string]*User)
	rooms      = make(map[string]*Room)
)

// 매치 메이킹 웹소켓 핸들러
func matchMaking() func(*fiber.Ctx) error {
	return websocket.New(func(c *websocket.Conn) {
		// c.Locals is added to the *websocket.Conn
		log.Println(c.Locals("allowed")) // true
		// log.Println(c.Cookies("session")) // ""

		// websocket.Conn bindings https://pkg.go.dev/github.com/fasthttp/websocket?tab=doc#pkg-index
		var (
			mt  int
			msg []byte
			err error
		)
		if mt, msg, err = c.ReadMessage(); err != nil {
			log.Println("read:", err)
		}
		fmt.Println(mt, string(msg))
		var user User
		// msg 변수를 User 구조체로 변환
		json.Unmarshal(msg, &user)
		user.conn = c
		// 추후 추가 : db에서 유저 정보를 끌어와서 레이팅을 설정한다.
		// user.Rating = rating

		// 대기열 채널에 User.ID를 추가 후 매칭을 기다림
		//	  채널에서 매칭이 되면, 두 유저의 정보를 받아서 방 구조체를 생성 후 맵에 추가
		//    생성된 방 구조체의 정보 (접속 경로, 유저 정보, 진영 정보)를 각각의 유저에게 전달

		fmt.Printf("user's pointer first check: %p \n", &user)
		matchMakingRequest(user)

		if mt, msg, err = c.ReadMessage(); err != nil {
			log.Println("read:", err)
		}
		fmt.Println(mt, string(msg), "====================")
		// client가 "done"을 보내면, 게임 방 형성이 완료되었다는 뜻이다.
		if string(msg) == "done" {
			// 다른 유저도 "done"을 보냈는지 확인한다.
			fmt.Println("check user who send done: ", &user)
			fmt.Printf("user's pointer second check: %p \n", &user)

			room := user.room

			return

			if room == nil {
				fmt.Println("room nil", user, room)
			}

			//slice에 append할 때를 대비 해 lock을 걸어두자

			fmt.Println(room.Users)

			rwMutex.Lock()
			for i := 0; i < len(room.Users); i++ {
				if room.Users[i] == nil {
					room.Users[i] = &user
					break
				}
			}
			rwMutex.Unlock()

			fmt.Println("check room users : ", room.Users)

			if len(room.Users) == 1 {

				// 30초 정도의 타임 아웃을 두고, "done"을 보내지 않는다면, 방의 상태를 2으로 변경한다.
				for time.Since(time.Now()).Seconds() < 30 {
					if len(room.Users) == 2 {
						fmt.Println("check ready for User in joining room : ", room.Users)
						// 방의 상태가 1로 변경되고, 게임이 시작된다.
						room.Status = 1
						go GameCommunication(user)
						break
					} else {
						room.Status = 2
						// 게임이 종료된 것이다. (타임 아웃)
						// 추후에 아래와 같은 로직을 추가한다.
						// 응답하지 않은 유저 디비에 경고 회수를 증가시키고
						// 응답한 유저는 재매칭을 시도하게 한다.
						continue
					}
				}
			}
			// 두 유저가 모두 "done"을 보냈다면, 방의 상태를 1로 변경한다.
			room.Status = 1
			go GameCommunication(user)
		}
	})
}

// 매치메이킹 채널에 유저 정보 전달
func matchMakingRequest(user User) {
	// matchMakingChannel = make(chan User)

	// matchQueue를 수신하는 채널에 user 정보 전달
	matchQueue <- &user
}

// goroutine으로 매치메이킹 채널을 계속해서 확인
func matchUsers() {
	for {
		select {
		case userInfo := <-matchQueue:
			// 매칭을 대기하는 유저 맵 생성
			users[userInfo.ID] = userInfo

			if users[userInfo.ID] == userInfo {
				fmt.Printf("pointer is same for : %p, %p \n", userInfo, users[userInfo.ID])
			}
			fmt.Println("match get user", &userInfo, "in users size of ", len(users))
			fmt.Println("users : ", users)
			fmt.Println("====================")
			// Try to find a match for the client
			for key := range users {
				otherUser := users[key]
				if userInfo.ID != otherUser.ID {
					// Found a match, handle the matchmaking logic here
					MatchUsersLogic(userInfo, otherUser)
				}
			}
		}
	}
}

func MatchUsersLogic(user1, user2 *User) {
	// Implement your matchmaking logic here
	// 추후 추가 : 레이팅을 비교해서 매칭을 시도한다.

	// 방 생성 후 User.conn 에 방 정보를 저장 (방법?)

	// UUID 생성
	// uuid

	room := &Room{
		ID:     "1",
		Users:  [2]*User{},
		Status: 0,
	}

	msg, err := json.Marshal(room)
	if err != nil {
		fmt.Println(err)
		return
	}

	// Room 정보를 각각의 user에 저장
	user1.room = room
	user2.room = room

	user1.side = 8
	user2.side = 16

	// For example, you can send a message to both clients that they are matched
	user1.conn.WriteMessage(websocket.TextMessage, makeMessage("matched", msg))
	user2.conn.WriteMessage(websocket.TextMessage, makeMessage("matched", msg))

	// Remove clients from the matchmaking queue and clients map
	delete(users, user1.ID)
	delete(users, user2.ID)

	fmt.Printf("check two user inMatchUsersLogic : %p, %p \n", &user1, &user2)
}

// 게임 진행을 담당하는 고루틴
func GameCommunication(user User) {
	fmt.Println("Game Start")
	for {
		game := Game{}
		room := user.room
		fmt.Println("check room : ", room.ID, room.Users, room.Status)

		_, msg, err := user.conn.ReadMessage()
		if err != nil {
			fmt.Println(err)
			return
		}

		json.Unmarshal(msg, &game)
		game.Move = diagonalSymmetry(game.Move[0], game.Move[1])
		msg, err = json.Marshal(game)
		if err != nil {
			fmt.Println(err)
			return
		}
		for _, otherUser := range room.Users {
			if user.ID != otherUser.ID {
				otherUser.conn.WriteMessage(websocket.TextMessage, makeMessage("move", msg))
			}
		}

		// 만약, game.Over가 true라면, 게임이 종료된 것이다.
		// room map에서 해당 방을 삭제한다.
		// 두 유저에게 게임 종료 정보를 전달하고, (추후)변화된 레이팅 값을 데이터베이스에 저장한다.
		if game.Over {
			room.Status = 2
			delete(rooms, room.ID)
			for _, otherUser := range room.Users {
				if user.ID != otherUser.ID {
					msg := []byte("Game Over")
					otherUser.conn.WriteMessage(websocket.TextMessage, makeMessage("end", msg))
				} else {
					msg := []byte("Game Over")
					user.conn.WriteMessage(websocket.TextMessage, makeMessage("end", msg))
				}
			}
			return
		}
	}
}

func makeMessage(action string, msg []byte) []byte {
	message := Message{}
	message.Action = action
	message.Msg = msg

	m, err := json.Marshal(&message)
	if err != nil {
		fmt.Println(err)
		return nil
	}
	return m
}

// // 프론트엔드에서 처리할 부분?
func diagonalSymmetry(x, y int) [2]int {
	return [2]int{9 - x, 8 - y}
}
