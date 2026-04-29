# generate_structs.py

structs = {
    "StaticDirConfig": 'type StaticDirConfig struct {\n\tDirPath string `yaml:"dir"`\n}',
    "FileConfig": 'type FileConfig struct {\n\tPath string `yaml:"path"`\n}',
    "ForwardConfig": "type ForwardConfig struct{}",
    "apiConfig": "type APIConfig struct{}",
    "ContentConfig": "type ContentConfig struct{}",
    "AuthLoginConfig": "type AuthLoginConfig struct{}",
    "AuthSignupConfig": "type AuthSignupConfig struct{}",
    "AuthLogoutConfig": "type AuthLogoutConfig struct{}",
    "StorageAccessConfig": "type StorageAccessConfig struct{}",
    "DependenciesConfig": "type DependenciesConfig struct{}",
    "ProcessConfig": "type ProcessConfig struct{}",
    "FileServerConfig": "type FileServerConfig struct{}",
}


def to_filename(name: str) -> str:
    """Convert PascalCase to snake_case.go"""
    import re

    snake = re.sub(r"(?<!^)(?=[A-Z])", "_", name).lower()
    return f"{snake}.go"


for name, body in structs.items():
    filename = to_filename(name)
    with open(filename, "w") as f:
        f.write("package routes\n")
        f.write(f"""
import (
    "net/http"
)
""")

        f.write("\n"+body+"\n")

        f.write(f"""
func (c *{name}) Render(data map[string]string) error {{
	return nil
}}

""")
        
        f.write(f"""
func (c *{name}) GetHandleFunc(path string)func(w http.ResponseWriter, r *http.Request) {{
    return func(w http.ResponseWriter, r *http.Request){{
    

    }}
}}

""")

        f.write(f"""
func (c *{name}) Validate() error {{
	return nil
}}

""")
        # f.write(body + "\n")
    print(f"Created {filename}")
