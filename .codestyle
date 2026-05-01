# VHCO Architecture & Code Style Guide

> **VHCO** — Vertical Hierarchy with Closed-world Operations
>
> A code organization pattern designed to minimize cognitive load, maximize testability, and make any project understandable from its folder structure alone.

---

## Table of Contents

1. [Core Philosophy](#core-philosophy)
2. [Top-Level Structure](#top-level-structure)
3. [The Flow](#the-flow)
4. [Module Responsibilities](#module-responsibilities)
5. [The Golden Rules](#the-golden-rules)
6. [Protocols: Who Defines What](#protocols-who-defines-what)
7. [Views and ViewModels](#views-and-viewmodels)
8. [Features as Structs of Capabilities](#features-as-structs-of-capabilities)
9. [Sample Project Structures](#sample-project-structures)
10. [Do's and Don'ts](#dos-and-donts)
11. [Worked Examples](#worked-examples)
12. [Reading a VHCO Project](#reading-a-vhco-project)
13. [FAQ](#faq)

---

## Core Philosophy

The goal of VHCO is to make code **easy to reason about** by enforcing one simple principle:

> **A function's scope is defined by its parameters. It has access to nothing else.**

Every function lives in its own closed world. Its inputs define what gets in, its outputs define what gets out, and inside the function there is no access to anything beyond that. This makes every function:

- Easy to test (no hidden dependencies)
- Easy to reason about (you only need to read the signature)
- Easy to maintain (changes don't ripple through the codebase)
- Easy to mock (the contract is the parameter list)

The folder structure exists to make the project **self-describing**. A new engineer should be able to answer fundamental questions about the project just by looking at the directories.

---

## Top-Level Structure

A VHCO project has **exactly six** top-level folders. No more. No less. **Period.**

```
project/
├── domain/         # What the system is
├── features/       # What the system can do (capabilities)
├── usecases/       # How the system does specific things
├── io/             # How users interact with it (views + view models)
├── infra/          # External implementation details (API, DB, storage)
└── orchestrator/   # How it all wires together (DI + composition)
```

**Do not create any other top-level folders.** Not `utils/`, not `helpers/`, not `shared/`, not `common/`, not `models/`. If you feel the need for one, it almost certainly belongs in one of the six above.

---

## The Flow

Data flows through the system in one direction:

```
┌────────┐  event   ┌───────────┐  call    ┌──────────────┐  call    ┌─────────┐  call    ┌────────┐
│  View  │ ───────▶ │ ViewModel │ ───────▶ │   UseCase    │ ───────▶ │ Feature │ ───────▶ │ Infra  │
│ (dumb) │          │  (logic)  │          │ (operation)  │          │ (caps)  │          │ (impl) │
└────────┘          └───────────┘          └──────────────┘          └─────────┘          └────────┘
                          │                       │                       │                    │
                          ▼                       ▼                       ▼                    ▼
                    declares protocol       declares protocol       declares protocol      provides raw
                    of what it needs        of what it needs        of what it needs       adapter
                          │                       │                       │
                          └───────────────────────┴───────────────────────┘
                                                  │
                                          ┌──────────────┐
                                          │ Orchestrator │
                                          │  (wires all) │
                                          └──────────────┘
```

**Critical:** Each layer declares the **protocols it needs** from the layer below. The orchestrator is the **only** place that knows about concrete implementations and wires them together at runtime.

---

## Module Responsibilities

### `domain/` — Entities

The fundamental data structures and types that describe what the system **is**.

- Pure data, no behavior beyond simple transformations
- No dependencies on any other module
- This is what someone reads first to understand "what is this project about?"

**Examples:** `User`, `Order`, `Document`, `Transaction`

### `features/` — Capabilities

What the system can do, expressed as capabilities. **A feature is a struct (or record) of functions** — its capabilities. It is not a class with a constructor that injects dependencies.

- A feature struct's fields are its capability functions
- It declares only the *shape* of what's possible — nothing about how
- It does not construct itself — the orchestrator builds the struct value
- This is what someone reads to understand "what does this project do?"

**Examples:** `Authentication`, `OrderManagement`, `DocumentSync`

> **Important:** There is no `AuthenticationImpl` in `features/`. The struct itself *is* the feature. The orchestrator constructs the struct value with the right closures plugged in.

### `usecases/` — Specific Operations

A specific operation that uses one or more feature capabilities.

- Declares the **protocol of the use case** (the contract callers will use)
- Declares the **shape of feature capabilities it needs** (function types or protocols)
- Does not know about views, view models, or infra
- Returns a result; never reaches up the stack
- This is what someone reads to understand "how does this project do things?"

**Examples:** `LoginWithEmail`, `PlaceOrder`, `SyncDocumentToCloud`

> **Important:** There is no `LoginWithEmailImpl` in `usecases/`. The use case folder declares the contract. The orchestrator builds the wired-up implementation.

### `io/` — Input/Output

Where the user meets the system. Split into two roles:

- **View (output layer)** — dumb. Renders only. Maps enums from the view model into actual user-visible text and visuals. Allowed to use `if`, `switch`, and `for` for rendering choices.
- **ViewModel (input handler)** — holds logic, declares the use-case protocols it needs, exposes state as enums and data (never as display strings).

This is what someone reads to understand "how do I interact with this project?"

### `infra/` — External Implementations

The real implementations of external services: HTTP clients, databases, secure storage, file system access, third-party SDKs.

- Has **no knowledge of the system** — does not import from `domain`, `features`, `usecases`, or `io`
- Provides raw adapters with no awareness of the protocols they'll eventually satisfy
- The orchestrator is the only thing that wraps infra into the closures that satisfy feature/use-case protocols
- Replaceable without touching anything else

**Examples:** `HttpApiClient`, `SqliteDatabase`, `KeychainSecureStorage`

### `orchestrator/` — The Wirer

The **only** place dependency injection and composition happens. The only module allowed to import concrete types from multiple other modules.

- Constructs concrete feature structs by plugging in closures that call infra
- Constructs concrete use case implementations by giving them feature capabilities
- Constructs view models by giving them use cases
- Builds views with their fully-wired view models
- The orchestrator is the **assembly diagram** of the system

> **Important rule:** Anything ending in `Impl` — anything whose job is to compose other pieces together — lives in `orchestrator/`. Other modules declare **shapes**. Orchestrator builds **wired-up instances**.

---

## The Golden Rules

These are non-negotiable.

### 1. Modules don't import each other

`usecases`, `features`, `io`, and `infra` live in their own worlds. They communicate through interfaces only. The orchestrator is the exception — it's allowed to import from all of them.

### 2. No new top-level folders

Six folders. That's it. If you think you need a seventh, you don't.

### 3. Views are dumb (but not brain-dead)

- No business logic
- No function parameters (no callbacks passed in — they observe the view model)
- They **may** use `if`, `switch`, `for` for rendering choices
- They map view-model enums to display text, colors, icons
- They never know what the system "does" — only how to draw it

### 4. ViewModels never produce display text

ViewModels return enums and data. The view decides how to display them. This keeps localization, theming, and copy changes in one place — the view.

### 5. Each layer declares the protocols it needs (consumer-owned interfaces)

A view model declares the use-case protocol it needs. A use case declares the feature-capability shape it needs. A feature declares nothing about infra — it's just a struct of functions, and the orchestrator builds the closures by wrapping infra. The orchestrator satisfies all of them.

### 6. Functions only have access to their parameters

If a function needs something, it must receive it as a parameter. No hidden globals, no service locators, no ambient context.

### 7. All `Impl`s live in the orchestrator

If a thing's job is to construct or compose other things, it belongs in `orchestrator/`. Features, use cases, and view models in their own modules define **shapes**, not wired-up assemblies.

### 8. Infra is replaceable

You should be able to swap your HTTP client, your database, or your secure storage without touching anything outside `infra/` and a few `make...` files in `orchestrator/`.

---

## Protocols: Who Defines What

This is the most important architectural decision in VHCO, and it's worth being explicit:

> **The consumer owns the protocol.**

Each layer declares the shape of what it needs from the layer below. The orchestrator is responsible for providing something that fits that shape.

### Why consumer-owned?

If features defined the protocols use cases must conform to, you'd couple use cases to features' assumptions. If use cases defined the protocols view models must conform to, you'd couple view models to use cases' assumptions. Both directions break the closed-world principle.

When the **consumer** owns the protocol:

- Each module is genuinely a closed world that says only "I need something shaped like this."
- Modules can be developed in parallel — IO can be built with mock use cases before any use case exists, because IO declared the shape it wants.
- Refactoring a feature doesn't force a refactor of every use case that uses it — the use case's expected shape stays stable; the orchestrator just adapts.
- It mirrors how Go interfaces and hexagonal architecture's "ports" work: the user of a thing defines the contract.

### What this looks like in practice

| Module | Declares | Receives (from orchestrator) |
|---|---|---|
| `io/login/` | `protocol LoginUseCase` (what the VM calls) | A value satisfying `LoginUseCase` |
| `usecases/login/` | The shape of the feature capabilities it needs (e.g. `AuthenticateFn`) | A function/value satisfying that shape |
| `features/auth/` | The struct of capability functions it offers | Built by the orchestrator from infra adapters |
| `infra/` | Nothing — provides raw external-system adapters | Nothing — it's the bottom |

Each module's protocols sit **next to the code that uses them**, in the same folder as the consumer. The orchestrator imports concrete types from `infra` and writes the closures that connect everything.

### A small mental model

Think of every VHCO module as a function:

```
input: protocols/shapes (what I need from below)
output: protocols/shapes (what I offer to above)
```

The orchestrator is the only thing that sees both ends of every module and connects them.

---

## Views and ViewModels

This is the area teams get wrong most often. Read carefully.

### Views

Views are **rendering only**. They observe a view model and display whatever it tells them to. They may use control flow (`if`, `switch`, `for`) for rendering choices, but never for business decisions.

```swift
// ✅ CORRECT — dumb view, but allowed to switch on enums for rendering
struct LoginView: View {
    @ObservedObject var viewModel: LoginViewModel

    var body: some View {
        VStack {
            TextField("Email", text: $viewModel.email)
            SecureField("Password", text: $viewModel.password)
            Button("Sign In") { viewModel.onSignInTapped() }

            // ✅ allowed: switch on a VM enum to choose what to render
            switch viewModel.state {
            case .idle:
                EmptyView()
            case .loading:
                ProgressView()
            case .error(let e):
                Text(message(for: e)).foregroundColor(.red)
            }
        }
    }

    // ✅ The view maps enums to display strings — NOT the view model
    private func message(for error: LoginError) -> String {
        switch error {
        case .invalidCredentials: return "Email or password is incorrect."
        case .networkUnavailable: return "Check your connection and try again."
        }
    }
}
```

```swift
// ❌ WRONG — view takes functions, has business logic, gets text from VM
struct LoginView: View {
    let onSignIn: (String, String) -> Void   // ❌ no callbacks as parameters
    let errorText: String?                    // ❌ VM should not produce display text

    var body: some View {
        // ❌ business validation in the view
        if email.contains("@") && password.count >= 8 {
            Button("Sign In") { onSignIn(email, password) }
        }
    }
}
```

### ViewModels

ViewModels hold logic, expose state as enums and data, and depend on a use-case protocol they themselves declare.

```swift
// io/login/LoginUseCase.swift
// The VM declares what it needs from below.
protocol LoginUseCase {
    func execute(email: String, password: String) async -> Result<User, LoginError>
}

enum LoginError {
    case invalidCredentials
    case networkUnavailable
    case unknown
}

enum LoginState {
    case idle
    case loading
    case success
    case error(LoginError)
}
```

```swift
// io/login/LoginViewModel.swift
@MainActor
final class LoginViewModel: ObservableObject {
    @Published var email: String = ""
    @Published var password: String = ""
    @Published var state: LoginState = .idle

    private let loginUseCase: LoginUseCase

    init(loginUseCase: LoginUseCase) {
        self.loginUseCase = loginUseCase
    }

    func onSignInTapped() {
        Task {
            state = .loading
            switch await loginUseCase.execute(email: email, password: password) {
            case .success: state = .success
            case .failure(let err): state = .error(err)
            }
        }
    }
}
```

Notice: `LoginUseCase` is declared in `io/login/`, **next to the view model that uses it**. The view model doesn't import from `usecases/`. The orchestrator will inject something that satisfies this protocol.

---

## Features as Structs of Capabilities

A feature is **not** a class with injected dependencies. A feature is a **struct of capability functions**. The functions themselves are constructed in the orchestrator with whatever dependencies they need closed over.

### Why a struct of functions?

- Capabilities become first-class, testable values
- The feature module declares only the *shape* of what's possible — no wiring noise
- Every capability is independently mockable (replace one closure)
- Composition becomes data — orchestrator literally builds the struct field by field
- No `FeatureImpl` class polluting the feature module

### Shape

```swift
// features/Authentication.swift
struct Authentication {
    var authenticate: (_ email: String, _ password: String) async throws -> User
    var logOut: () async throws -> Void
    var refreshSession: () async throws -> Session
}
```

That's the entire feature module. It declares what the feature *can do*. There is no `AuthenticationImpl` here. There are no constructors that take infra. There is just the shape.

### The orchestrator builds it

```swift
// orchestrator/features/makeAuthentication.swift
func makeAuthentication(api: HttpAuthApi, tokens: KeychainTokenStore) -> Authentication {
    Authentication(
        authenticate: { email, password in
            let resp = try await api.postLogin(email: email, password: password)
            try tokens.save(resp.token)
            return User(id: resp.userId, email: resp.email)
        },
        logOut: {
            try tokens.clear()
        },
        refreshSession: {
            // ...
        }
    )
}
```

### What the use case receives

A use case declares the shape of feature capability it needs as a function type. It does not care that the function came from a struct.

```swift
// usecases/login/LoginWithEmail.swift
protocol LoginWithEmail {
    func execute(email: String, password: String) async -> Result<User, LoginError>
}

// The shape of what this use case needs from features:
typealias AuthenticateFn = (_ email: String, _ password: String) async throws -> User
```

The orchestrator passes `auth.authenticate` directly to the use case. The use case never knows the function came from a struct called `Authentication`.

---

## Sample Project Structures

### Small App: Todo List

```
todo-app/
├── domain/
│   ├── Todo.swift
│   └── TodoList.swift
│
├── features/
│   ├── TodoStorage.swift                # struct of capability functions
│   └── TodoSync.swift                   # struct of capability functions
│
├── usecases/
│   ├── AddTodo.swift                    # protocol + capability shape it needs
│   ├── ToggleTodo.swift
│   ├── DeleteTodo.swift
│   └── ListTodos.swift
│
├── io/
│   ├── todo-list/
│   │   ├── TodoListView.swift           # dumb
│   │   ├── TodoListViewModel.swift      # logic
│   │   └── TodoListUseCases.swift       # protocols the VM needs
│   └── add-todo/
│       ├── AddTodoView.swift
│       ├── AddTodoViewModel.swift
│       └── AddTodoUseCases.swift
│
├── infra/
│   ├── SqlitePersistence.swift          # raw sqlite wrapper
│   └── HttpClient.swift                 # raw HTTP
│
└── orchestrator/
    ├── AppOrchestrator.swift            # top-level entry
    ├── features/
    │   ├── makeTodoStorage.swift        # builds the TodoStorage struct
    │   └── makeTodoSync.swift
    ├── usecases/
    │   ├── makeAddTodo.swift            # builds AddTodo impl
    │   └── makeListTodos.swift
    └── views/
        ├── makeTodoListView.swift       # builds view + VM wired up
        └── makeAddTodoView.swift
```

### Medium App: Banking

```
banking-app/
├── domain/
│   ├── Account.swift
│   ├── Transaction.swift
│   ├── Money.swift
│   └── Customer.swift
│
├── features/
│   ├── Authentication.swift
│   ├── Accounts.swift
│   ├── Transfers.swift
│   └── Statements.swift
│
├── usecases/
│   ├── auth/
│   │   ├── LoginWithPassword.swift
│   │   ├── LogOut.swift
│   │   └── RefreshSession.swift
│   ├── accounts/
│   │   ├── ListAccounts.swift
│   │   └── GetAccountDetails.swift
│   └── transfers/
│       ├── InitiateTransfer.swift
│       └── ConfirmTransfer.swift
│
├── io/
│   ├── login/
│   ├── account-list/
│   ├── account-detail/
│   ├── transfer-flow/
│   └── statements/
│
├── infra/
│   ├── api/HttpBankingApi.swift
│   ├── storage/KeychainSecureStorage.swift
│   ├── storage/SqliteCache.swift
│   └── analytics/SegmentAnalytics.swift
│
└── orchestrator/
    ├── AppOrchestrator.swift
    ├── features/                        # builds feature structs
    ├── usecases/                        # builds use case impls
    └── views/                           # builds views
```

---

## Do's and Don'ts

### ✅ Do

- Keep views dumb. Render only. Map enums to text inside the view.
- Let views use `if`, `switch`, `for` — but only for rendering, never for business decisions.
- Define a feature as a struct of capability functions, not a class.
- Put every `Impl`, every wiring file, every `make...` factory in `orchestrator/`.
- Declare protocols **next to the consumer** that needs them, not next to the producer.
- Name use cases as verbs: `PlaceOrder`, `RefreshSession`, `ExportReport`.
- Name features as capabilities: `Authentication`, `OrderManagement`.
- Name domain entities as nouns: `Order`, `Customer`, `Invoice`.
- Make every infra class a thin adapter to an external system, with no awareness of the system's protocols.
- Test view models with mock use case protocols. Test use case impls (which live in orchestrator) with mock feature capabilities. Test feature struct construction with mock infra.

### ❌ Don't

- Don't add a seventh top-level folder. Not `shared/`, not `utils/`, not `common/`.
- Don't pass functions as parameters to views.
- Don't put business logic in views.
- Don't return display strings from view models.
- Don't put `Impl` files anywhere except `orchestrator/`.
- Don't put `FeatureImpl` classes in `features/`. The struct itself *is* the feature.
- Don't declare protocols next to the producer. Protocols belong with the consumer.
- Don't import a concrete class outside of `orchestrator/`.
- Don't let `infra/` know about `domain/`, `features/`, `usecases/`, or `io/`.
- Don't let use cases know about views or view models.
- Don't reach for global singletons or service locators. Inject everything.
- Don't put domain logic in `infra/`. Infra adapts external systems and nothing else.
- Don't create a "models" folder. Entities go in `domain/`. DTOs that are infra-specific stay in `infra/`.

---

## Worked Examples

### Example: A login flow, end to end

**The user types email + password and taps "Sign In."**

#### `domain/User.swift`
```swift
struct User {
    let id: String
    let email: String
}
```

#### `features/Authentication.swift`
```swift
// Just the shape. No impl. No constructor.
struct Authentication {
    var authenticate: (_ email: String, _ password: String) async throws -> User
    var logOut: () async throws -> Void
}
```

#### `usecases/login/LoginWithEmail.swift`
```swift
// Protocol the consumers (orchestrator/io) will satisfy/expect.
protocol LoginWithEmail {
    func execute(email: String, password: String) async -> Result<User, LoginError>
}

enum LoginError: Error {
    case invalidCredentials
    case networkUnavailable
    case unknown
}

// The shape of feature capability this use case needs.
// Orchestrator will pass auth.authenticate here.
typealias AuthenticateFn = (_ email: String, _ password: String) async throws -> User
```

#### `io/login/LoginUseCase.swift`
```swift
// The VM is the consumer here. It declares the protocol it needs.
// (Often this is identical in shape to the use case protocol — that's fine.
// They're independent declarations from each layer's perspective. The
// orchestrator hands the VM a value that satisfies LoginUseCase.)
protocol LoginUseCase {
    func execute(email: String, password: String) async -> Result<User, LoginError>
}
```

#### `io/login/LoginViewModel.swift`
```swift
@MainActor
final class LoginViewModel: ObservableObject {
    @Published var email = ""
    @Published var password = ""
    @Published var state: LoginState = .idle

    private let loginUseCase: LoginUseCase

    init(loginUseCase: LoginUseCase) {
        self.loginUseCase = loginUseCase
    }

    func onSignInTapped() {
        Task {
            state = .loading
            switch await loginUseCase.execute(email: email, password: password) {
            case .success: state = .success
            case .failure(let err): state = .error(err)
            }
        }
    }
}

enum LoginState {
    case idle, loading, success
    case error(LoginError)
}
```

#### `io/login/LoginView.swift`
```swift
struct LoginView: View {
    @ObservedObject var viewModel: LoginViewModel

    var body: some View {
        VStack(spacing: 16) {
            TextField("Email", text: $viewModel.email)
            SecureField("Password", text: $viewModel.password)
            Button("Sign In") { viewModel.onSignInTapped() }

            switch viewModel.state {
            case .idle, .success: EmptyView()
            case .loading: ProgressView()
            case .error(let err): Text(message(for: err)).foregroundColor(.red)
            }
        }
        .padding()
    }

    private func message(for error: LoginError) -> String {
        switch error {
        case .invalidCredentials: return "Email or password is incorrect."
        case .networkUnavailable: return "Please check your connection."
        case .unknown: return "Something went wrong. Try again."
        }
    }
}
```

#### `infra/HttpAuthApi.swift`
```swift
// Just an adapter to the network. Knows nothing about the system.
final class HttpAuthApi {
    private let session: URLSession
    init(session: URLSession) { self.session = session }

    func postLogin(email: String, password: String) async throws -> (userId: String, email: String, token: String) {
        // raw HTTP call returning a raw response shape
    }
}

// Just an adapter to keychain. Knows nothing about the system.
final class KeychainTokenStore {
    func save(_ token: String) throws { /* keychain write */ }
    func read() throws -> String? { /* keychain read */ }
    func clear() throws { /* keychain delete */ }
}
```

#### `orchestrator/features/makeAuthentication.swift`
```swift
// Takes raw infra adapters, returns a wired-up feature struct.
func makeAuthentication(
    api: HttpAuthApi,
    tokens: KeychainTokenStore
) -> Authentication {
    Authentication(
        authenticate: { email, password in
            let resp = try await api.postLogin(email: email, password: password)
            try tokens.save(resp.token)
            return User(id: resp.userId, email: resp.email)
        },
        logOut: {
            try tokens.clear()
        }
    )
}
```

#### `orchestrator/usecases/makeLoginWithEmail.swift`
```swift
// Takes the feature capability function it needs, returns a use case impl
// that satisfies BOTH the usecase-side protocol and the io-side protocol.
func makeLoginWithEmail(authenticate: @escaping AuthenticateFn) -> LoginWithEmailImpl {
    LoginWithEmailImpl(authenticate: authenticate)
}

final class LoginWithEmailImpl: LoginWithEmail, LoginUseCase {
    private let authenticate: AuthenticateFn
    init(authenticate: @escaping AuthenticateFn) {
        self.authenticate = authenticate
    }
    func execute(email: String, password: String) async -> Result<User, LoginError> {
        do {
            let user = try await authenticate(email, password)
            return .success(user)
        } catch {
            return .failure(.unknown) // map specific errors here
        }
    }
}
```

#### `orchestrator/views/makeLoginView.swift`
```swift
func makeLoginView() -> LoginView {
    let api = HttpAuthApi(session: .shared)
    let tokens = KeychainTokenStore()

    let auth = makeAuthentication(api: api, tokens: tokens)
    let loginUseCase = makeLoginWithEmail(authenticate: auth.authenticate)

    let viewModel = LoginViewModel(loginUseCase: loginUseCase)
    return LoginView(viewModel: viewModel)
}
```

#### `orchestrator/AppOrchestrator.swift`
```swift
final class AppOrchestrator {
    func rootView() -> some View {
        makeLoginView()
    }
}
```

Read this carefully and notice:

- `features/Authentication.swift` is just a struct of function fields. No `Impl`. No constructor.
- `infra/` provides raw adapters with zero system knowledge.
- `usecases/` declares contracts (`LoginWithEmail` protocol + `AuthenticateFn` type alias). No `Impl`.
- `io/` declares the protocol the VM needs (`LoginUseCase`) and the dumb view that renders enums.
- `orchestrator/` is the **only** place where things get constructed and wired. Every `Impl`, every `make...` factory, every closure that bridges one module to another lives here.

---

## Reading a VHCO Project

When you open a new VHCO codebase, this is the reading order:

| Question | Where to look |
|---|---|
| What is this project about? | `domain/` |
| What can it do? | `features/` |
| How does it do specific things? | `usecases/` |
| How do users interact with it? | `io/` |
| What external systems does it talk to? | `infra/` |
| How is it all wired together? | `orchestrator/` |

If a folder structure can answer all six questions in under five minutes, the architecture is doing its job.

---

## FAQ

**Q: Where do shared utilities go (date formatters, string helpers)?**
A: If it's used by one module, it lives next to where it's used. If it's a domain concept (e.g., `Money` formatting), it lives in `domain/`. If it wraps a platform API, it's in `infra/`. Resist the urge to make a `utils/` folder.

**Q: Where do constants and config go?**
A: Configuration values are infra (env URLs, API keys). Domain constants live with the domain entity they relate to.

**Q: Can a feature use another feature?**
A: No. If feature A needs feature B's capability, that's a sign you have a use case that composes them. Use cases compose features, not features composing features.

**Q: Where do DTOs (data transfer objects) live?**
A: API request/response shapes live in `infra/` next to the client that uses them. The orchestrator's feature-builder closures map them into domain entities at the boundary.

**Q: My view needs a small piece of derived state. Can I compute it in the view?**
A: If it's purely visual (e.g., a color based on a state enum), yes. If it involves business rules, it goes in the view model.

**Q: Can two view models share state?**
A: They shouldn't share mutable state directly. Both depend on the same use case, and the use case reads from a feature whose underlying source is the single source of truth.

**Q: Where do tests go?**
A: Mirror the structure under a `tests/` folder at the repo root. Each module is testable in isolation because every dependency is a protocol or function value.

**Q: Should the use case protocol live in `usecases/` or `io/`?**
A: The consumer owns the protocol. The view model is the consumer of "a thing that performs login," so the protocol it calls into lives in `io/`. The `usecases/` folder also declares a protocol because the orchestrator is its consumer when constructing the impl. The two protocols may be identically shaped — that's fine. They're independent declarations from each layer's perspective, and the orchestrator's `Impl` simply conforms to both.

**Q: Why not just have one shared protocol used by both `io/` and the orchestrator?**
A: Because that would force `io/` to import from `usecases/`, breaking the closed-world rule. The minor duplication of a protocol shape is the price of true module isolation. In practice, these protocols rarely drift — and when they do, that's signal worth paying attention to.

**Q: What if I really, really need a seventh folder?**
A: You don't. Re-read the responsibilities of the six. If something doesn't fit, it's almost always:
- A capability → `features/`
- An entity → `domain/`
- A specific operation → `usecases/`
- A wiring concern → `orchestrator/`
- A platform/external concern → `infra/`
- A user-facing concern → `io/`

---

## Summary

VHCO is strict on purpose. The strictness is what gives you:

- Predictable folder structure across every project
- Trivially testable code (every dependency is a protocol or function value)
- Replaceable infrastructure
- Self-documenting architecture
- Bounded cognitive load when reading any single file

The three things to internalize:

1. **Six folders, no exceptions.**
2. **Consumer owns the protocol.** Each layer declares the shape of what it needs from below.
3. **All wiring lives in `orchestrator/`.** Features are structs of functions, view models hold logic, views render — and the orchestrator is the only place that knows how to assemble them.

Follow the rules. The discipline pays for itself the first time someone new joins the team and is productive within a day.