# The Complete Rules of React

> **Source:** [react.dev/reference/rules](https://react.dev/reference/rules) (React v19.2)

React has its own idioms — or **rules** — for how to express patterns in a way that is easy to understand and yields high-quality applications. These are rules, not just guidelines — if they are broken, your app likely has bugs. They are also the foundation that **React Compiler** relies on to automatically optimize your code.

---

## Category 1: Components and Hooks Must Be Pure

Purity makes your code predictable, easy to debug, and allows React to automatically optimize your components and Hooks correctly. React may render components multiple times to create the best possible user experience — purity is what makes this safe.

### Rule 1 — Components and Hooks Must Be Idempotent

Given the same inputs (props, state, context), your component must **always return the same output**. Functions like `new Date()` or `Math.random()` produce different results each time — they must not be called during render.

```jsx
// ❌ Bad: always returns a different result
function Clock() {
  const time = new Date();
  return <span>{time.toLocaleString()}</span>;
}

// ✅ Good: non-idempotent code moved outside of render
function Clock() {
  const [time, setTime] = useState(() => new Date());

  useEffect(() => {
    const id = setInterval(() => setTime(new Date()), 1000);
    return () => clearInterval(id);
  }, []);

  return <span>{time.toLocaleString()}</span>;
}
```

### Rule 2 — Side Effects Must Run Outside of Render

Side effects are code with any observable effect other than returning a value — DOM manipulation, data fetching, logging, etc. They should **never** run during render. Use **event handlers** or **`useEffect`** instead.

```jsx
// ❌ Bad: side effect during render
function ProductDetailPage({ product }) {
  document.title = product.title;
}

// ✅ Good: side effect in useEffect
function ProductDetailPage({ product }) {
  useEffect(() => {
    document.title = product.title;
  }, [product.title]);
}
```

**Exception — Local mutation is fine:**

```jsx
function FriendList({ friends }) {
  const items = []; // created locally
  for (let i = 0; i < friends.length; i++) {
    items.push(<Friend key={friends[i].id} friend={friends[i]} />);
  }
  return <section>{items}</section>;
}
```

This is safe because `items` is created fresh every render and never leaks outside the component.

### Rule 3 — Props Are Immutable

Never mutate props directly. If you need a modified version, create a copy.

```jsx
// ❌ Bad: mutating props
function Post({ item }) {
  item.url = new Url(item.url, base);
  return <Link url={item.url}>{item.title}</Link>;
}

// ✅ Good: creating a copy
function Post({ item }) {
  const url = new Url(item.url, base);
  return <Link url={url}>{item.title}</Link>;
}
```

### Rule 4 — State Is Immutable

Never assign to state variables directly. Always use the setter function returned by `useState`.

```jsx
// ❌ Bad: direct mutation
function Counter() {
  const [count, setCount] = useState(0);
  function handleClick() {
    count = count + 1; // UI won't update
  }
}

// ✅ Good: using the setter
function Counter() {
  const [count, setCount] = useState(0);
  function handleClick() {
    setCount(count + 1); // React queues a re-render
  }
}
```

### Rule 5 — Return Values and Arguments to Hooks Are Immutable

Once values are passed to a Hook, don't modify them. Hooks may memoize based on those arguments, so mutating them silently breaks caching.

```jsx
// ❌ Bad: mutating hook arguments
function useIconStyle(icon) {
  const theme = useContext(ThemeContext);
  if (icon.enabled) {
    icon.className = computeStyle(icon, theme);
  }
  return icon;
}

// ✅ Good: making a copy
function useIconStyle(icon) {
  const theme = useContext(ThemeContext);
  const newIcon = { ...icon };
  if (icon.enabled) {
    newIcon.className = computeStyle(icon, theme);
  }
  return newIcon;
}
```

### Rule 6 — Values Are Immutable After Being Passed to JSX

Don't mutate objects after they've been used in a JSX expression. React may evaluate JSX eagerly, so later mutations won't be reflected. Move mutations to **before** the JSX is created.

```jsx
// ❌ Bad: mutating after JSX usage
function Page({ colour }) {
  const styles = { colour, size: "large" };
  const header = <Header styles={styles} />;
  styles.size = "small"; // too late — already used above
  const footer = <Footer styles={styles} />;
  return <>{header}<Content />{footer}</>;
}

// ✅ Good: separate values
function Page({ colour }) {
  const headerStyles = { colour, size: "large" };
  const header = <Header styles={headerStyles} />;
  const footerStyles = { colour, size: "small" };
  const footer = <Footer styles={footerStyles} />;
  return <>{header}<Content />{footer}</>;
}
```

---

## Category 2: React Calls Components and Hooks

React is responsible for rendering components and hooks when necessary to optimize the user experience. It is **declarative** — you tell React what to render, and React figures out how.

### Rule 7 — Never Call Component Functions Directly

Components should only be used in JSX. Don't call them as regular functions.

```jsx
// ❌ Bad: calling as a function
function App() {
  return BlogPost(); // React can't manage this properly
}

// ✅ Good: using in JSX
function App() {
  return <BlogPost />;
}
```

When you call a component directly, React doesn't create a node in the component tree for it — no hooks, no lifecycle, no proper reconciliation.

### Rule 8 — Never Pass Hooks Around as Regular Values

Hooks should only be called inside of components or other custom hooks. Never store them in variables, pass them as arguments, or call them dynamically.

```jsx
// ❌ Bad: passing a hook as a value
function ChatInput() {
  const useSettings = getSettings ? useDesktopSettings : useMobileSettings;
  const settings = useSettings();
}

// ✅ Good: call hooks directly
function ChatInput() {
  const desktopSettings = useDesktopSettings();
  const mobileSettings = useMobileSettings();
  const settings = getSettings ? desktopSettings : mobileSettings;
}
```

---

## Category 3: Rules of Hooks

Hooks are JavaScript functions that represent a special type of reusable UI logic with restrictions on where they can be called.

### Rule 9 — Only Call Hooks at the Top Level

Don't call Hooks inside loops, conditions, or nested functions. Always use Hooks at the top level of your React function, before any early returns.

```jsx
// ❌ Bad: hook inside a condition
function Form() {
  const [name, setName] = useState('Mary');

  if (name !== '') {
    useEffect(function persistForm() {
      localStorage.setItem('formData', name);
    });
  }

  const [surname, setSurname] = useState('Poppins');
}

// ✅ Good: condition inside the hook
function Form() {
  const [name, setName] = useState('Mary');

  useEffect(function persistForm() {
    if (name !== '') {
      localStorage.setItem('formData', name);
    }
  });

  const [surname, setSurname] = useState('Poppins');
}
```

React tracks hooks by **call order**. If a hook is conditionally skipped, every subsequent hook shifts position and returns the wrong state.

### Rule 10 — Only Call Hooks from React Functions

Don't call Hooks from regular JavaScript functions. You can only call them from:

- React function components
- Custom Hooks (functions whose name starts with `use`)

```jsx
// ❌ Bad: hook in a regular function
function calculateTotal(items) {
  const [total, setTotal] = useState(0); // not a React component or hook
}

// ✅ Good: hook in a custom hook
function useTotal(items) {
  const [total, setTotal] = useState(0);
  // ...
  return total;
}
```

---

## Quick Reference Table

| #  | Rule                                          | Category       | Key Takeaway                                    |
|----|-----------------------------------------------|----------------|-------------------------------------------------|
| 1  | Components must be idempotent                 | Purity         | Same inputs → same output                       |
| 2  | Side effects outside of render                | Purity         | React may re-render at any time                 |
| 3  | Don't mutate props                            | Purity         | Causes inconsistent, hard-to-debug output       |
| 4  | Don't mutate state directly                   | Purity         | UI won't update — use the setter                |
| 5  | Don't mutate Hook arguments or return values  | Purity         | Breaks memoization and caching                  |
| 6  | Don't mutate values after passing to JSX      | Purity         | JSX may be evaluated eagerly                    |
| 7  | Never call components as functions            | React Control  | Bypasses React's tree management                |
| 8  | Never pass Hooks as regular values            | React Control  | Hooks have strict call-site restrictions         |
| 9  | Hooks at the top level only                   | Hooks          | React tracks hooks by call order                |
| 10 | Hooks only from React functions               | Hooks          | Enables React to track state and effects        |

---

## Why These Matter for React Compiler

React Compiler (formerly "React Forget") statically analyzes your code at build time and automatically inserts memoization (`useMemo`, `useCallback`, `React.memo` equivalents). It **assumes you are following all 10 rules above**. If you violate them:

- The compiler may **skip** your component entirely (leaving it un-optimized)
- The compiler may produce **incorrect** optimizations (cached values that should have been recalculated)
- Your app may exhibit **subtle bugs** that are hard to trace

This is why these went from "best practices" to hard **rules** — they're the contract between your code and React's optimization engine.

### Enforcing the Rules

Use these tools to catch violations automatically:

- **`<StrictMode>`** — Renders components twice in development to surface impure render logic
- **`eslint-plugin-react-hooks`** — Enforces Rules of Hooks and exhaustive dependency arrays
- **React Compiler ESLint rules** — Additional lint rules that catch purity violations the compiler cares about

---

## ESLint Configuration for Next.js 16+

> **Breaking change:** Next.js 16 removed the `next lint` command entirely. `next build` no longer runs linting automatically. You must now run ESLint directly (e.g., via an npm script like `"lint": "eslint ."`).

Next.js 16 uses ESLint 9's **flat config** format (`eslint.config.mjs`) instead of the legacy `.eslintrc` format. The old JSON-based config will not work.

### Option 1: Using `eslint-config-next` (Recommended)

`eslint-config-next` still bundles `eslint-plugin-react`, `eslint-plugin-react-hooks`, and `@next/eslint-plugin-next` together. This is the simplest setup:

```js
// eslint.config.mjs
import { defineConfig } from 'eslint/config'
import nextVitals from 'eslint-config-next/core-web-vitals'

const eslintConfig = defineConfig([
  ...nextVitals,
  {
    rules: {
      // Override specific rules if needed
      'react/no-unescaped-entities': 'off',
      '@next/next/no-page-custom-font': 'off',
    },
  },
])

export default eslintConfig
```

For TypeScript projects, add the TypeScript-specific config:

```js
// eslint.config.mjs
import { defineConfig } from 'eslint/config'
import nextVitals from 'eslint-config-next/core-web-vitals'
import nextTypescript from 'eslint-config-next/typescript'

const eslintConfig = defineConfig([
  ...nextVitals,
  ...nextTypescript,
])

export default eslintConfig
```

### Option 2: Manual Plugin Setup (Full Control)

If you prefer to configure each plugin individually:

```js
// eslint.config.mjs
import { defineConfig } from 'eslint/config'
import reactPlugin from 'eslint-plugin-react'
import reactHooksPlugin from 'eslint-plugin-react-hooks'
import nextPlugin from '@next/eslint-plugin-next'

const eslintConfig = defineConfig([
  {
    files: ['**/*.{js,jsx,ts,tsx}'],
    plugins: {
      react: reactPlugin,
      'react-hooks': reactHooksPlugin,
      '@next/next': nextPlugin,
    },
    rules: {
      ...reactPlugin.configs['jsx-runtime'].rules,
      ...reactHooksPlugin.configs.recommended.rules,
      ...nextPlugin.configs.recommended.rules,
      ...nextPlugin.configs['core-web-vitals'].rules,
    },
    settings: {
      react: { version: 'detect' },
    },
  },
  {
    ignores: ['.next/*'],
  },
])

export default eslintConfig
```

### Option 3: React Compiler Lint Rules (Bleeding Edge)

`eslint-plugin-react-hooks` now includes compiler-powered rules that go beyond the classic two hook rules. These catch violations of purity, immutability, ref access during render, and more:

```js
// eslint.config.mjs
import reactHooks from 'eslint-plugin-react-hooks'
import { defineConfig } from 'eslint/config'

export default defineConfig([
  // Use 'recommended-latest' for all compiler-powered rules
  reactHooks.configs.flat['recommended-latest'],
])
```

The `recommended-latest` preset enables rules like:

| Rule                              | What It Catches                                |
|-----------------------------------|------------------------------------------------|
| `react-hooks/rules-of-hooks`     | Hooks called conditionally or in loops         |
| `react-hooks/exhaustive-deps`    | Missing or extra dependencies in effects       |
| `react-hooks/purity`             | Impure code during render (e.g. `Math.random()`)|
| `react-hooks/immutability`       | Mutating props, state, or hook arguments       |
| `react-hooks/refs`               | Accessing ref `.current` during render         |
| `react-hooks/set-state-in-render`| Calling `setState` during render               |
| `react-hooks/set-state-in-effect`| Unnecessary `setState` in effects              |
| `react-hooks/static-components`  | Components that could be hoisted out           |

### pnpm Users: Known Issue

pnpm's strict dependency hoisting can cause `eslint-plugin-react-hooks` to not be found, even when it's a transitive dependency of `eslint-config-next`. The fix is to install it explicitly:

```bash
pnpm add -D eslint-plugin-react-hooks
```

### Removed: `eslint` Option in `next.config`

The `eslint` configuration option in `next.config.js` / `next.config.ts` is no longer needed in Next.js 16 and can be safely removed:

```diff
// next.config.ts
const nextConfig = {
-  eslint: {
-    dirs: ['src'],
-    ignoreDuringBuilds: true,
-  },
}
```