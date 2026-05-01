import * as Vue from "./vue3.js";

/**
 * ComponentLoader - Dynamic Vue component loading system
 * Supports lazy loading of components with templates, handlers, and scoped CSS
 */
class ComponentLoader {
    constructor() {
        this.loadedComponents = new Map();
        this.loadingPromises = new Map();
        this.componentStylesheets = new Map();
        this.scopeIdCounter = 0;
        this.cache = new Map();
        this.componentRegistry = new Map();
        this.securityConfig = {
            allowScripts: true,
            allowStyles: true,
            trustedDomains: [],
            maxFileSize: 1024 * 1024, // 1MB limit
        };
    }

    registerComponentSource(name, config) {
        this.componentRegistry.set(name, config);
    }

    configureSecurity(config) {
        Object.assign(this.securityConfig, config);
    }

    async mountApp(appConfig = {}, selector = "#app") {
        const state = {
            state: appConfig.state ? Vue.reactive(appConfig.state) : {}
        };

        delete appConfig["state"]
        let data = {}
        if (appConfig.data){
            delete appConfig["data"]
            data = appConfig.data()
        }


        const mountFn = async () => {
            try {
                const app = Vue.createApp({
                    data() {
                        return {...state, ...data};
                    },
                    ...appConfig
                });

                await this.registerComponents(app, state.state, appConfig.imports || []);

                const mountedApp = app.mount(selector);
                console.log("App mounted successfully:", { app, mountedApp });
                return app;
            } catch (error) {
                console.error("Failed to mount app:", error);
                this.handleMountError(error, selector);
                throw error;
            }
        };

        if (document.readyState === "loading") {
            return new Promise((resolve, reject) => {
                document.addEventListener("DOMContentLoaded", async () => {
                    try {
                        const app = await mountFn();
                        resolve(app);
                    } catch (error) {
                        reject(error);
                    }
                });
            });
        } else {
            console.log("DOM already loaded");
            return mountFn();
        }
    }

    async registerComponents(app, state, imports) {
        console.log("Registering components:", { imports });

        const importMap = new Map();
        imports.forEach(item => {
            if (item.target) {
                importMap.set(item.target, item);
            }
        });

        app._componentImportMap = importMap;

        const registrationPromises = imports.map(item =>
            this.registerSingleComponent(app, state, item, importMap)
        );

        const results = await Promise.allSettled(registrationPromises);

        results.forEach((result, index) => {
            if (result.status === 'rejected') {
                console.error(`Failed to register component ${imports[index].target}:`, result.reason);
            }
        });

        const successCount = results.filter(r => r.status === 'fulfilled').length;
        console.log(`Successfully registered ${successCount}/${imports.length} components`);
    }

    async registerSingleComponent(app, state, item, importMap) {
        const { target, view, handler } = item;

        if (!target || !view) {
            throw new Error(`Invalid component config: target="${target}", view="${view}"`);
        }

        if (app._registeredComponents?.has(target)) {
            return true;
        }

        try {
            const html = await this.fetchHTML(view);
            const { template, props, scopedCSS, components, scopeId } = this.parseTemplate(html);

            // Recursively register nested components
            if (components && components.length > 0) {
                const nestedPromises = components.map(async (compName) => {
                    const nestedItem = importMap.get(compName);
                    if (!nestedItem) {
                        console.warn(
                            `Component "${target}" references unknown nested component "${compName}". ` +
                            `Make sure it's included in the imports array.`
                        );
                        return;
                    }
                    await this.registerSingleComponent(app, state, nestedItem, importMap);
                });
                await Promise.all(nestedPromises);
            }

            let componentOptions = { template };

            if (handler) {
                const module = await this.loadHandler(handler);
                let lifecycle = {};
                if (module) {
                    if (typeof module == "function") {
                        lifecycle = module(state);
                    } else if (module.init) {
                        lifecycle = module.init(state);
                    }
                }
                componentOptions = { ...componentOptions, ...lifecycle };
            } else {
                componentOptions = { ...componentOptions, props };
            }

            // Handle scoped CSS
            if (scopedCSS && scopeId) {
                this.injectScopedCSS(target, scopedCSS, scopeId);
                componentOptions.template = this.applyScopeIdToTemplate(template, scopeId);
            }

            if (!app._registeredComponents) app._registeredComponents = new Set();
            app._registeredComponents.add(target);

            app.component(target, componentOptions);
            console.log(`Component "${target}" registered successfully`);
            return true;

        } catch (error) {
            console.error(`Error registering component ${target}:`, error);
            throw error;
        }
    }

    async fetchHTML(url) {
        try {
            if (this.cache.has(url)) return this.cache.get(url);

            const response = await fetch(url);
            if (!response.ok) throw new Error(`HTTP ${response.status}: ${response.statusText}`);

            const contentLength = response.headers.get('content-length');
            if (contentLength && parseInt(contentLength) > this.securityConfig.maxFileSize) {
                throw new Error(`File size exceeds maximum allowed (${this.securityConfig.maxFileSize} bytes)`);
            }

            const cachedHTML = await response.text();
            this.cache.set(url, cachedHTML);
            return cachedHTML;
        } catch (error) {
            console.error(`Failed to fetch HTML from ${url}:`, error);
            throw error;
        }
    }

    async loadHandler(url) {
        try {
            const module = await import(url);
            return module.default || module;
        } catch (error) {
            console.error(`Failed to load handler from ${url}:`, error);
            throw error;
        }
    }

    parseTemplate(html) {
        const parser = new DOMParser();
        const doc = parser.parseFromString(html, "text/html");

        const templateEl = doc.querySelector("template");
        if (!templateEl) throw new Error("No <template> element found in component HTML");

        const template = templateEl.innerHTML.trim();
        const propsAttr = templateEl.getAttribute("props");
        const componentsAttr = templateEl.getAttribute("components");

        let props = propsAttr
            ? propsAttr.split(/\s*,\s*/).map(p => p.trim()).filter(Boolean)
            : [];

        let components = componentsAttr
            ? componentsAttr.split(/\s*,\s*/).map(c => c.trim()).filter(Boolean)
            : [];

        // Scoped styles
        let scopedCSS = null;
        let scopeId = null;

        const styleEls = [...doc.querySelectorAll('style')];
        if (styleEls.length > 0) {
            const scopedCssArr = [];
            const nonScopedCssArr = [];

            for (const element of styleEls) {
                const textContent = element.innerHTML || "";
                if (element.hasAttribute("scoped")) {
                    scopedCssArr.push(textContent);
                } else {
                    nonScopedCssArr.push(textContent);
                }
            }

            if (scopedCssArr.length > 0) {
                scopeId = `data-v-${this.generateScopeId()}`;
                scopedCSS = scopedCssArr.join('\n');
            }

            if (nonScopedCssArr.length > 0) {
                const style = document.createElement("style");
                style.innerHTML = nonScopedCssArr.join('\n');
                document.head.appendChild(style);
            }
        }

        return { template, props, scopedCSS, components, scopeId };
    }

    generateScopeId() {
        return (++this.scopeIdCounter).toString(36);
    }

    processScopedCSS(css, scopeId) {
        return css
            .split('}')
            .filter(rule => rule.trim() !== '')
            .map(rule => {
                const [selectors, body] = rule.split('{');
                if (!body) return rule + '}';
                const scopedSelectors = selectors
                    .split(',')
                    .map(sel => sel.trim())
                    .filter(Boolean)
                    .map(sel => {
                        // Handle ::v-deep or >>> for deep selectors
                        if (sel.includes('::v-deep') || sel.includes('>>>')) {
                            return sel.replace(/::v-deep|>>>/g, '').trim();
                        }

                        // Split selector into parts (e.g., "div .class > span" -> ["div", ".class", ">", "span"])
                        const parts = sel.split(/(\s+|>|\+|~)/);

                        // Add scope to the last meaningful selector part
                        let lastSelectorIndex = -1;
                        for (let i = parts.length - 1; i >= 0; i--) {
                            if (parts[i].trim() && !/^\s+$|^[>+~]$/.test(parts[i])) {
                                lastSelectorIndex = i;
                                break;
                            }
                        }

                        if (lastSelectorIndex >= 0) {
                            parts[lastSelectorIndex] = parts[lastSelectorIndex] + `[${scopeId}]`;
                        }

                        return parts.join('');
                    })
                    .join(', ');
                return `${scopedSelectors} {${body}}`;
            })
            .join('');
    }

    injectScopedCSS(componentName, css, scopeId) {
        const processedCSS = this.processScopedCSS(css, scopeId);
        const style = document.createElement('style');
        style.textContent = processedCSS;
        style.setAttribute('data-component', componentName);
        document.head.appendChild(style);
        this.componentStylesheets.set(componentName, style);
    }

    applyScopeIdToTemplate(template, scopeId) {
        const wrapper = document.createElement('div');
        wrapper.innerHTML = template;

        function applyScope(el) {
            if (el.nodeType === 1) { // Element node
                el.setAttribute(scopeId, '');
                // Recursively apply to all descendants
                Array.from(el.children).forEach(child => applyScope(child));
            }
        }

        Array.from(wrapper.children).forEach(el => applyScope(el));
        return wrapper.innerHTML;
    }

    handleMountError(error, selector) {
        const mountPoint = document.querySelector(selector);
        if (!mountPoint) {
            console.error(`Mount point "${selector}" not found in DOM`);
        }
        if (process.env.NODE_ENV !== 'production' && mountPoint) {
            mountPoint.innerHTML = `
                <div style="padding: 20px; background: #fee; border: 2px solid #c00; border-radius: 4px;">
                    <h3 style="margin: 0 0 10px 0; color: #c00;">Failed to Mount App</h3>
                    <pre style="margin: 0; overflow: auto;">${error.message}</pre>
                </div>
            `;
        }
    }

    unregisterComponent(name) {
        this.loadedComponents.delete(name);
        const stylesheet = this.componentStylesheets.get(name);
        if (stylesheet) {
            stylesheet.remove();
            this.componentStylesheets.delete(name);
        }
    }

    clearComponents() {
        this.loadedComponents.clear();
        this.componentStylesheets.forEach(stylesheet => stylesheet.remove());
        this.componentStylesheets.clear();
    }
}

const componentLoader = new ComponentLoader();

export async function mountApp(appConfig = {}, selector = "#app") {
    return componentLoader.mountApp(appConfig, selector);
}

export {
    Vue
}